package main

import (
	_ "embed"
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

//go:embed index.html
var indexHTML []byte

type FileItem struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

type FileResponse struct {
	QrCode      string     `json:"qrCode"`
	URL         string     `json:"url"`
	CurrentPath string     `json:"currentPath"`
	IsRoot      bool       `json:"isRoot"`
	Items       []FileItem `json:"items"`
}

var (
	clients      = make(map[net.Conn]bool)
	clientsMutex sync.Mutex
	broadcast    = make(chan string)
	logMutex     sync.Mutex

	targetDir string
	baseDir   string // the directory wifiler was launched from; browsing can never go above this
	dirMutex  sync.RWMutex

	// secretKey is generated fresh every time the server starts. Anyone who
	// wants access must have scanned the current QR code (or been told the
	// key) - it is never persisted to disk.
	secretKey string

	// lockPort is a fixed loopback-only port used purely to detect whether
	// another wifiler instance is already running on this machine.
	lockPort = "47591"
)

const sessionCookieName = "wifiler_session"

func writeLog(message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedLine := fmt.Sprintf("[%s] %s", timestamp, message)
	fmt.Println(formattedLine)

	logMutex.Lock()
	defer logMutex.Unlock()
	// Write log safely to the user's home profile so it doesn't get left scattered in active folders
	logPath := filepath.Join(os.Getenv("USERPROFILE"), "wifiler_log.txt")
	if runtime.GOOS != "windows" {
		logPath = filepath.Join(os.Getenv("HOME"), "wifiler_log.txt")
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(formattedLine + "\n")
	}
}

func getLocalIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ipStr := ipnet.IP.String()
				if len(ipStr) >= 7 && ipStr[:7] == "169.254" {
					continue
				}
				return ipStr
			}
		}
	}
	return "127.0.0.1"
}

// isRunningInWSL detects WSL (WSL1 or WSL2). Under WSL2's default NAT
// networking, the address getLocalIP() finds is only reachable from inside
// Windows/WSL itself - a phone on the same Wi-Fi has no route to it, so the
// QR code silently fails to load. We can't fix that from inside the
// process, but we can warn about it clearly instead of leaving it a
// confusing dead end.
func isRunningInWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	v := strings.ToLower(string(data))
	return strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
}

// generateSessionKey creates a random per-run access key. 8 random bytes
// (16 hex chars) is small enough to type by hand if needed but large
// enough that guessing it isn't practical within a session's lifetime.
func generateSessionKey() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely, but never fail startup over this.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// acquireSingleInstanceLock ensures only one wifiler process runs at a time
// on this machine by trying to bind a fixed loopback-only port. If the bind
// fails, another instance already holds it.
func acquireSingleInstanceLock() net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:"+lockPort)
	if err != nil {
		fmt.Println("wifiler is already running on this machine. Only one instance is allowed at a time.")
		os.Exit(1)
	}
	return ln
}

// isAuthed checks the session cookie against the current run's secretKey.
func isAuthed(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	return err == nil && cookie.Value == secretKey
}

// requireAuth wraps a handler so it 401s unless the caller has a valid
// session cookie (obtained by loading "/" with the correct ?key=).
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthed(r) {
			http.Error(w, "Unauthorized - please rescan the QR code shown on the host computer", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func watchDirectory() {
	var lastStateString string
	for {
		time.Sleep(1 * time.Second)
		dirMutex.RLock()
		currentPath := targetDir
		dirMutex.RUnlock()

		files, err := os.ReadDir(currentPath)
		if err != nil {
			continue
		}

		var trackingSignature string
		for _, f := range files {
			if f.Name() != "wifiler_log.txt" && f.Name() != "wifiler.exe" && f.Name()[0] != '.' {
				info, err := f.Info()
				if err == nil {
					trackingSignature += fmt.Sprintf("%s-%d-%t;", f.Name(), info.Size(), f.IsDir())
				}
			}
		}

		if trackingSignature != lastStateString {
			lastStateString = trackingSignature
			broadcast <- "reload"
		}
	}
}

func handleNotifications() {
	for {
		msg := <-broadcast
		clientsMutex.Lock()
		for client := range clients {
			frame := []byte{0x81, byte(len(msg))}
			frame = append(frame, []byte(msg)...)
			client.Write(frame)
		}
		clientsMutex.Unlock()
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !isAuthed(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	h := sha1.New()
	io.WriteString(h, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	cryptoSum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + cryptoSum + "\r\n\r\n")
	bufrw.Flush()

	clientsMutex.Lock()
	clients[conn] = true
	clientsMutex.Unlock()

	go func() {
		defer func() {
			clientsMutex.Lock()
			delete(clients, conn)
			clientsMutex.Unlock()
			conn.Close()
		}()
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				break
			}
		}
	}()
}

const version = "1.0.0"

func printHelp() {
	fmt.Print(`wifiler - Wi-Fi File Server

Usage:
  wifiler                  Share the current directory
  wifiler --dir <path>     Share a specific directory instead of the current one
  wifiler --port <port>    Try this port first instead of the default (8080)
  wifiler version          Print the version and exit
  wifiler uninstall        Remove wifiler from this machine
  wifiler help             Show this help message
`)
}

// logFilePath returns the path writeLog() uses, so uninstall can clean it up.
func logFilePath() string {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, "wifiler_log.txt")
}

func confirmUninstall(exePath, installDir, logPath string) bool {
	fmt.Println("This will remove:")
	if runtime.GOOS == "windows" {
		fmt.Printf("  - the program folder: %s\n", installDir)
		fmt.Println("  - the wifiler entry from your PATH environment variable")
	} else {
		fmt.Printf("  - the program binary: %s\n", exePath)
	}
	fmt.Printf("  - the log file: %s\n", logPath)
	fmt.Print("Continue? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}

// scheduleWindowsCleanup writes a small helper .bat file that waits for this
// process to exit, then removes the whole install folder and strips it out
// of the user's PATH. Doing this as a real script (rather than one giant
// chained command string passed through exec.Command) avoids two problems
// that broke the previous approach: cmd.exe's !VAR! delayed-expansion
// syntax is off by default unless a script explicitly turns it on, and long
// quoted one-liners are easy for Windows to re-parse incorrectly.
func scheduleWindowsCleanup(installDir string) error {
	batPath := filepath.Join(os.TempDir(), "wifiler_uninstall.bat")

	batContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
ping 127.0.0.1 -n 3 > nul
cd /d "%%TEMP%%"
rmdir /S /Q "%s"
for /f "tokens=2*" %%%%A in ('reg query "HKCU\Environment" /v PATH 2^>nul') do set "OLD_PATH=%%%%B"
set "NEW_PATH=!OLD_PATH:%s;=!"
set "NEW_PATH=!NEW_PATH:;%s=!"
reg add "HKCU\Environment" /v PATH /t REG_SZ /d "!NEW_PATH!" /f
del /F /Q "%%~f0"
`, installDir, installDir, installDir)

	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		return err
	}

	cmd := exec.Command("cmd.exe", "/C", batPath)
	return cmd.Start()
}

// removeWithSudoFallback tries a plain os.Remove first (the common case for
// files under $HOME, like the log). If that fails specifically because of a
// permissions error - which is expected for the binary, since install.sh
// installs it to /usr/local/bin with sudo - it retries via `sudo rm -f`,
// with stdin/stdout/stderr wired to the terminal so sudo can prompt for a
// password right there.
func removeWithSudoFallback(path string) (removed bool, err error) {
	err = os.Remove(path)
	if err == nil {
		return true, nil
	}
	if !os.IsPermission(err) {
		return false, err
	}

	fmt.Printf("Removing %s requires elevated permissions - you may be asked for your password.\n", path)
	cmd := exec.Command("sudo", "rm", "-f", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		return false, runErr
	}
	return true, nil
}

func selfUninstall() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Could not determine wifiler's install location:", err)
		return
	}
	installDir := filepath.Dir(exePath)
	logPath := logFilePath()

	if !confirmUninstall(exePath, installDir, logPath) {
		fmt.Println("Uninstall cancelled. Nothing was removed.")
		return
	}

	// Remove the log file up front - this process can do it directly and
	// immediately, unlike the binary/folder below on Windows.
	logRemoved := false
	logMissing := false
	if err := os.Remove(logPath); err == nil {
		logRemoved = true
	} else if os.IsNotExist(err) {
		logMissing = true
	} else if os.IsPermission(err) {
		removed, removeErr := removeWithSudoFallback(logPath)
		logRemoved = removed
		if !removed {
			fmt.Printf("Could not remove log file (%v)\n", removeErr)
		}
	} else {
		fmt.Printf("Could not remove log file (%v)\n", err)
	}

	if runtime.GOOS == "windows" {
		// The running exe can't delete itself (or its own folder) while
		// still executing, so hand off a short delayed script instead.
		scheduleErr := scheduleWindowsCleanup(installDir)

		fmt.Println("\nRemoving wifiler:")
		if scheduleErr == nil {
			fmt.Printf("  [scheduled] program folder: %s\n", installDir)
			fmt.Println("  [scheduled] PATH environment variable entry")
		} else {
			fmt.Printf("  [failed] could not schedule folder/PATH removal: %v\n", scheduleErr)
			fmt.Printf("  You can remove it manually: %s\n", installDir)
		}
		if logRemoved {
			fmt.Printf("  [done]      log file: %s\n", logPath)
		} else if logMissing {
			fmt.Printf("  [skipped]   log file: %s (not found)\n", logPath)
		} else {
			fmt.Printf("  [failed]    log file: %s\n", logPath)
		}
		if scheduleErr == nil {
			fmt.Println("\nThese finish a couple of seconds after this window closes. Uninstall complete.")
		}
		os.Exit(0)
	} else {
		binRemoved, removeErr := removeWithSudoFallback(exePath)
		if !binRemoved {
			fmt.Printf("Could not remove binary (%v)\n", removeErr)
			fmt.Printf("You can remove it manually with: sudo rm %s\n", exePath)
		}

		fmt.Println("\nRemoving wifiler:")
		if binRemoved {
			fmt.Printf("  [done] program binary: %s\n", exePath)
		} else {
			fmt.Printf("  [failed] program binary: %s\n", exePath)
		}
		if logRemoved {
			fmt.Printf("  [done] log file: %s\n", logPath)
		} else if logMissing {
			fmt.Printf("  [skipped] log file: %s (not found)\n", logPath)
		} else {
			fmt.Printf("  [failed] log file: %s\n", logPath)
		}

		if binRemoved && (logRemoved || logMissing) {
			fmt.Println("\nUninstall complete. Everything has been removed.")
		} else {
			fmt.Println("\nUninstall finished with errors - see above.")
		}
		os.Exit(0)
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "uninstall":
			selfUninstall()
			return
		case "version", "--version", "-v":
			fmt.Println("wifiler version " + version)
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}

	// Optional flags: --port <n> and --dir <path>. Anything unrecognized
	// is ignored rather than failing the whole run.
	requestedPort := "8080"
	requestedDir := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				requestedPort = args[i+1]
				i++
			}
		case "--dir":
			if i+1 < len(args) {
				requestedDir = args[i+1]
				i++
			}
		}
	}
	if _, err := strconv.Atoi(requestedPort); err != nil {
		fmt.Printf("Invalid --port value %q, must be a number.\n", requestedPort)
		os.Exit(1)
	}

	// Only one wifiler process may run at a time.
	lockLn := acquireSingleInstanceLock()
	defer lockLn.Close()

	secretKey = generateSessionKey()

	if requestedDir != "" {
		abs, err := filepath.Abs(requestedDir)
		if err != nil {
			fmt.Printf("Invalid --dir value: %v\n", err)
			os.Exit(1)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			fmt.Printf("--dir %q is not a valid directory.\n", requestedDir)
			os.Exit(1)
		}
		targetDir = abs
	} else {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			log.Fatalf("Failed setup: %v", err)
		}
	}
	baseDir = targetDir

	localIP := getLocalIP()

	// Dynamic porting: prefer the requested/default port, but fall back to
	// any free OS-assigned port if it's already taken instead of refusing
	// to start.
	ln, err := net.Listen("tcp", ":"+requestedPort)
	if err != nil {
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			log.Fatalf("Failed to bind any port: %v", err)
		}
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	shareURL := fmt.Sprintf("http://%s:%s/?key=%s", localIP, port, secretKey)

	qrBytes, _ := qrcode.Encode(shareURL, qrcode.Medium, 256)
	base64QR := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrBytes)

	go watchDirectory()
	go handleNotifications()

	// Root page: authenticates the browser. If a valid ?key= is present,
	// issue a session cookie; otherwise fall back to any existing cookie.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		keyParam := r.URL.Query().Get("key")
		if keyParam != "" && keyParam == secretKey {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    secretKey,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		} else if !isAuthed(r) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("<h2>Access Denied</h2><p>Please scan the QR code shown on the host computer.</p>"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})

	http.HandleFunc("/ws", handleWebSocket)

	// API: Directory Listing Endpoint
	http.HandleFunc("/api/files", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		dirMutex.RLock()
		currPath := targetDir
		dirMutex.RUnlock()

		var items []FileItem
		// "Root" is the directory wifiler was launched from - browsing can
		// never go above it, so the "up" button hides there.
		isAtBase := currPath == baseDir

		files, _ := os.ReadDir(currPath)
		for _, f := range files {
			if f.Name() != "wifiler.exe" && f.Name() != "wifiler" && (len(f.Name()) == 0 || f.Name()[0] != '.') {
				items = append(items, FileItem{Name: f.Name(), IsDir: f.IsDir()})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FileResponse{
			QrCode:      base64QR,
			URL:         shareURL,
			CurrentPath: currPath,
			IsRoot:      isAtBase,
			Items:       items,
		})
	}))

	// API: Step Inside a Subdirectory
	http.HandleFunc("/api/cd", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		targetFolder := r.URL.Query().Get("target")

		// Only a single path segment is allowed - no traversal, no jumping
		// straight to an arbitrary ancestor/sibling directory.
		if targetFolder == "" || strings.ContainsAny(targetFolder, "/\\") || strings.Contains(targetFolder, "..") {
			http.Error(w, "invalid target", http.StatusBadRequest)
			return
		}

		dirMutex.Lock()
		candidate := filepath.Join(targetDir, targetFolder)
		// Belt-and-braces: the candidate must still be inside baseDir.
		rel, relErr := filepath.Rel(baseDir, candidate)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			dirMutex.Unlock()
			http.Error(w, "invalid target", http.StatusBadRequest)
			return
		}
		targetDir = candidate
		writeLog(fmt.Sprintf("Moved into: %s", targetDir))
		dirMutex.Unlock()

		broadcast <- "reload"
		w.Write([]byte(`{"success":true}`))
	}))

	// API: Jump Up a Directory (never above baseDir)
	http.HandleFunc("/api/cd/up", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}

		dirMutex.Lock()
		if targetDir != baseDir {
			targetDir = filepath.Dir(targetDir)
		}
		writeLog(fmt.Sprintf("Moved up to: %s", targetDir))
		dirMutex.Unlock()

		broadcast <- "reload"
		w.Write([]byte(`{"success":true}`))
	}))

	// API: Dynamic File Upload
	http.HandleFunc("/api/upload", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}

		dirMutex.RLock()
		currPath := targetDir
		dirMutex.RUnlock()

		r.ParseMultipartForm(32 << 20)
		files := r.MultipartForm.File["files"]

		for _, fileHeader := range files {
			func() {
				file, err := fileHeader.Open()
				if err != nil {
					return
				}
				defer file.Close()

				filename := filepath.Base(fileHeader.Filename)
				if filename == "wifiler.exe" || filename == "wifiler" || filename == "wifiler_log.txt" {
					writeLog(fmt.Sprintf("Blocked upload attempting to overwrite protected file: %s", filename))
					return
				}
				out, err := os.Create(filepath.Join(currPath, filename))
				if err != nil {
					return
				}
				defer out.Close()
				io.Copy(out, file)
				writeLog(fmt.Sprintf("Saved upload: %s", filename))
			}()
		}
		w.Write([]byte(`{"success":true}`))
	}))

	// Secure Static Downloading Router
	http.HandleFunc("/files/", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		dirMutex.RLock()
		currPath := targetDir
		dirMutex.RUnlock()

		rawPath := r.URL.Path[len("/files/"):]
		fullPath := filepath.Join(currPath, rawPath)

		// Verify the resolved path is actually inside currPath rather than
		// just blacklisting ".." substrings.
		rel, err := filepath.Rel(currPath, fullPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.Error(w, "Invalid file token traversal", http.StatusForbidden)
			return
		}

		http.ServeFile(w, r, fullPath)
	}))

	fmt.Printf("\n===================================================\n")
	fmt.Printf("     wifiler v%s - Wi-Fi File Server\n", version)
	fmt.Printf("===================================================\n")
	if isRunningInWSL() {
		fmt.Println("WARNING: You're running wifiler inside WSL.")
		fmt.Printf("  The address below (%s) is WSL's internal address and is\n", localIP)
		fmt.Println("  usually unreachable from your phone, even on the same Wi-Fi.")
		fmt.Println("  Fix: enable WSL2 mirrored networking - add this to")
		fmt.Println("  %UserProfile%\\.wslconfig on Windows, then run 'wsl --shutdown'")
		fmt.Println("  and restart WSL:")
		fmt.Println("      [wsl2]")
		fmt.Println("      networkingMode=mirrored")
		fmt.Println("  Or simplest: run wifiler.exe directly on Windows instead of WSL.")
		fmt.Println()
	}
	fmt.Printf("Session key: %s\n", secretKey)
	fmt.Printf("(Only devices that scan the QR code below or are given this key can connect)\n\n")
	terminalQR, _ := qrcode.New(shareURL, qrcode.Medium)
	fmt.Println(terminalQR.ToSmallString(false))

	log.Fatal(http.Serve(ln, nil))
}