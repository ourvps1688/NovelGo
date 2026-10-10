#!/bin/bash
set -euo pipefail

VERSION="${1:-dev}"
APP_NAME="novelgo"
APPDIR="build/${APP_NAME}.AppDir"
RUNTIME_DIR="build/runtime"

rm -rf "$APPDIR"
mkdir -p "$APPDIR"

# 二进制
cp "build/bin/$APP_NAME" "$APPDIR/"

# 运行时依赖
cp -r "$RUNTIME_DIR" "$APPDIR/"

# 桌面入口
cat > "$APPDIR/${APP_NAME}.desktop" <<EOF
[Desktop Entry]
Name=NovelGo
Comment=智能写作助手
Exec=novelgo
Icon=novelgo
Type=Application
Categories=Office;WordProcessor;
Terminal=false
EOF

# 图标（优先专用图标，回退到项目 appicon.png）
if [ -f build/package/linux/novelgo.png ]; then
  cp build/package/linux/novelgo.png "$APPDIR/"
elif [ -f appicon.png ]; then
  cp appicon.png "$APPDIR/novelgo.png"
else
  echo "Warning: no icon found, AppImage will have no icon" >&2
  touch "$APPDIR/novelgo.png"
fi

# AppRun
cat > "$APPDIR/AppRun" <<'APPRUN'
#!/bin/bash
HERE="$(dirname "$(readlink -f "${0}")")"
exec "${HERE}/novelgo" "$@"
APPRUN
chmod +x "$APPDIR/AppRun" "$APPDIR/novelgo"

# 下载 appimagetool
if [ ! -f /tmp/appimagetool ]; then
    curl -fsSL -o /tmp/appimagetool \
        "https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-$(uname -m).AppImage"
    chmod +x /tmp/appimagetool
fi

# 生成 AppImage（--appimage-extract-and-run 避免 FUSE 依赖）
ARCH=$(uname -m)
mkdir -p build/dist
/tmp/appimagetool --appimage-extract-and-run "$APPDIR" "build/dist/novelgo-v${VERSION}-linux-${ARCH}.AppImage"

echo "AppImage → build/dist/novelgo-v${VERSION}-linux-${ARCH}.AppImage"
