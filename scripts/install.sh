# Run this command on Mac/Linux:
# curl -fsSL https://raw.githubusercontent.com/akashkumarpr/wifiler/main/scripts/install.sh | bash

#!/bin/bash
REPO="akashkumarpr/wifiler"
VERSION="v1.0.0"
OS_TYPE="$(uname -s)"
ARCH_TYPE="$(uname -m)"

if [ "$OS_TYPE" = "Darwin" ] && [ "$ARCH_TYPE" = "arm64" ]; then BINARY="wifiler-mac-arm64"
elif [ "$OS_TYPE" = "Darwin" ]; then BINARY="wifiler-mac-intel"
else BINARY="wifiler-linux"; fi

echo "Downloading wifiler for $OS_TYPE..."
sudo curl -L -o /usr/local/bin/wifiler "https://github.com/$REPO/releases/download/$VERSION/$BINARY"
sudo chmod +x /usr/local/bin/wifiler
echo "[done] wifiler installed successfully! Run 'wifiler'."