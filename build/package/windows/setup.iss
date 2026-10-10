#define MyAppName "NovelGo"
#define MyAppVersion GetEnv("VERSION")
#define MyAppExeName "novelgo.exe"
; VERSION_INFO 是规范化后的 4 段数字版本号（如 1.4.2.0），由 Makefile/CI 注入，
; 用于 VersionInfoVersion/VersionInfoProductVersion，未传时回退 0.0.0.0
#define MyVersionInfo GetEnv("VERSION_INFO")
#if MyVersionInfo == ""
  #define MyVersionInfo "0.0.0.0"
#endif

[Setup]
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=ourvps1688
AppPublisherURL=https://github.com/ourvps1688/NovelGo
AppSupportURL=https://github.com/ourvps1688/NovelGo/issues
AppUpdatesURL=https://github.com/ourvps1688/NovelGo/releases
AppId=NovelGo
DefaultDirName={code:GetDefaultDir}
DefaultGroupName={#MyAppName}
OutputDir=..\..\dist
OutputBaseFilename=novelgo-v{#MyAppVersion}-windows-amd64
Compression=lzma2
SolidCompression=yes
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\{#MyAppExeName}
ArchitecturesInstallIn64BitMode=x64compatible
DirExistsWarning=no
WizardStyle=modern
; PE 版本资源（安装包 exe 右键属性显示版本/公司/版权）
VersionInfoVersion={#MyVersionInfo}
VersionInfoProductVersion={#MyVersionInfo}
VersionInfoCompany=novelgo
VersionInfoDescription=NovelGo - AI 小说创作助手
VersionInfoProductName=NovelGo
VersionInfoCopyright=Copyright © 2026 novelgo
VersionInfoOriginalFilename=novelgo-v{#MyAppVersion}-windows-amd64.exe
; UsePreviousAppDir 默认 yes，AppId 稳定后升级时自动读取上次安装目录

[Files]
Source: "..\..\bin\novelgo.exe"; DestDir: "{app}"; Flags: ignoreversion
; VC++ Runtime DLL（vcruntime140/vcruntime140_1/msvcp140）是 onnxruntime.dll 的依赖。
; Windows 加载器解析 onnxruntime.dll 的依赖时从 exe 目录（标准搜索顺序第 1 位）查找，
; 不搜 onnxruntime.dll 所在目录，故单独装到 {app}（novelgo.exe 同目录），runtime 里排除。
Source: "..\..\runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs; Excludes: "vcruntime140.dll,vcruntime140_1.dll,msvcp140.dll"
Source: "..\..\runtime\vcruntime140.dll";   DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\runtime\vcruntime140_1.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\runtime\msvcp140.dll";       DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式:"; Flags: checkedonce

[Icons]
Name: "{autoprograms}\{#MyAppName}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autoprograms}\{#MyAppName}\卸载 NovelGo"; Filename: "{uninstallexe}"
Name: "{userdesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

; 安装完成后可选启动 NovelGo（postinstall 显示复选框，skipifsilent 避免静默安装时弹窗）
[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "启动 NovelGo"; Flags: postinstall skipifsilent

[Code]
function GetDefaultDir(Param: string): string;
begin
  if DirExists('D:\') then Result := 'D:\NovelGo'
  else if DirExists('E:\') then Result := 'E:\NovelGo'
  else Result := 'C:\NovelGo';
end;
