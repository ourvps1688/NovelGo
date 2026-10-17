// 面板路由标识。activePanel 是主区渲染的 14 种面板之一。
// 来源：WorkspaceView 否定链排除的 11 个专门 View（characters/locations/
// storyarcs/timeline/reader/preferences/novel-settings/profile/git/style-samples/
// bookpitch）+ BookshelfView 分支（novels）+ ContentPanel 默认（chapters、skills）。
export type PanelId =
  | "novels"
  | "chapters"
  | "chapter-management"
  | "characters"
  | "locations"
  | "storyarcs"
  | "timeline"
  | "reader"
  | "preferences"
  | "novel-settings"
  | "profile"
  | "git"
  | "skills"
  | "skill-matrix"
  | "style-samples"
  | "signability"
  | "bookpitch";

// 侧栏可额外为 "search"（搜索面板覆盖在 SidePanel 内）。
export type SidebarPanelId = PanelId | "search";
