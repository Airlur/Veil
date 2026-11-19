# AGENTS.md — 后续 Agent / 开发者迭代指南

> 本文件是给 AI Agent 和后续维护者的工程上下文。改代码前先读完，能避开大量已经踩过的坑。

## 项目是什么

Veil（隐幕）：Windows 被控端的"防窥隐私屏保"。运行后本机物理屏被一张图盖住，但远控软件（UU 远程、ToDesk 等）看到的仍是真实桌面，鼠标键盘照常操作。替代 UU 远程的付费防窥功能。

**技术路线**：覆盖层窗口 + `SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)`。不是虚拟显示器方案——这是理解所有设计取舍的前提。

## 核心机制（改代码前必读）

遮罩窗口 = 一个手写的 Win32 窗口（`overlay.go`），同时满足四件事，缺一不可：

1. **捕获排除**：`SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE /* 0x11 */)`。要求 Win10 2004+。
2. **鼠标穿透**：扩展样式必须 `WS_EX_LAYERED | WS_EX_TRANSPARENT` **两个一起**。单独 `WS_EX_TRANSPARENT` 只影响绘制顺序，跨进程点击穿透不生效（表现为远控端点击全部失效）。
3. **LAYERED 窗口必须激活渲染**：加了 `WS_EX_LAYERED` 后必须调用 `SetLayeredWindowAttributes(hwnd, 0, 255, LWA_ALPHA)`，否则窗口**根本不渲染**（症状：开启遮罩后屏幕毫无变化，但日志全绿）。且绝不能调用 `UpdateLayeredWindow` / 使用真分层渲染——那会让捕获排除退化。
4. **不抢焦点**：`WS_EX_NOACTIVATE` + `ShowWindow(SW_SHOWNOACTIVATE)`。

WDA 与 LAYERED 的兼容性已实测：只声明样式 + 普通 GDI 绘制时共存正常。

## 架构与文件职责

| 文件 | 职责 |
|---|---|
| `main.go` | 入口、单实例（重启语义）、DPI 声明、消息循环、日志 |
| `overlay.go` | 遮罩窗口创建/绘制/WNDPROC、控制窗口（托盘/热键/跨线程命令入口）、开启关闭逻辑 |
| `hotkey.go` | 可自定义全局热键：解析、注册、冲突回滚、兜底键 |
| `tray.go` | 托盘图标、右键菜单 |
| `settings.go` | 设置面板（WebView2 常驻实例）、图片校验、文件对话框 |
| `presets.go` | 内置预设代码生成、cover 缩放、目录预设扫描、缩略图 |
| `toast.go` | 自绘启动提示条（不依赖系统通知） |
| `monitor.go` | 显示器枚举 |
| `config.go` | 配置读写（exe 同目录 config.json） |
| `win32.go` | 全部 Win32 API 声明/常量/结构体 |
| `ui/index.html` | 设置面板 UI（go:embed 进 exe） |
| `tools/genlogo` | 生成 exe 图标（黑底白 V，来自用户 SVG） |
| `tools/genmenu` | 生成菜单图标 PNG（方框+闭眼，来自用户 SVG） |
| `assets/menu/eye.png` | 菜单图标源文件（go:embed） |
| `presets/` | 目录预设（运行时扫描，随程序分发） |

## 已踩过的坑（每条都真实发生过）

1. **DPI**：所有以"物理像素"传给 Win32 的尺寸（窗口、toast、绘制区域 RECT）都必须乘 `GetDpiForSystem()` 缩放。硬编码在 200% 屏幕上必然出现"内容被压扁一半 / 文字被裁"。WebView2 的 CSS 视口跟随 DPI：想要 800 逻辑宽的布局，窗口物理宽要 1600。
2. **`GetModuleHandleW` 在 kernel32 不在 user32**——放错 DLL 直接 panic。
3. **PNG 解码类型**：`png.Decode` 返回 `*image.NRGBA`（不是 `*image.RGBA`），硬断言会 panic（曾导致右键托盘整个程序崩溃）。统一用 `draw.Draw` 转换。
4. **`SetMenuItemInfo` 的 hbmpItem 不支持 per-pixel alpha**：透明像素渲染成黑。菜单图标必须白底不透明。
5. **圆角矩形 SDF**：用标准公式（outside+inside-r），旧公式对内部点返回 -r 导致整个内部被误填充。
6. **托盘 VERSION_4 事件**：一次右键会先后发 `WM_RBUTTONUP` 和 `WM_CONTEXTMENU`，只处理后者，否则菜单弹两次。
7. **message-only 窗口**：`FindWindowW` 找不到（HWND_MESSAGE 窗口不在枚举里）。单实例通知需要可枚举窗口，控制窗口用普通隐藏窗口（`WS_EX_TOOLWINDOW` 无 WS_VISIBLE）。
8. **WebView2 白屏**：面板首次打开约 1s 初始化延迟，实例常驻（关闭=隐藏）后二次秒开。`Eval` 必须经 `Dispatch` 到 webview 线程。
9. **鼠标钩子方案已废弃**：早期用 WH_MOUSE_LL 吞物理鼠标移动事件（只放行带 LLMHF_INJECTED 的远程注入事件），实测导致本地光标位置与点击落点脱节，且 UU 的真实机制是隐藏光标而非拦截事件。若将来要做"遮罩期间隐藏光标"，正确做法是 `SetSystemCursor` 换成透明光标（注意崩溃恢复机制），不要走钩子。hook.go 已删除。
10. **前端后端返回格式**：绑定函数成功也要返回合法 JSON，前端统一 parse，否则刷新逻辑被跳过。
11. **UI 改动必须自查**：用 playwright-cli（msedge）渲染 `ui/index.html` + mock 数据截图检查后再交付。`file:` 协议被拦，用本地 http.server 提供页面。

## 热键系统

- 三组可自定义（开关/设置/退出）+ 一组固定兜底（`Ctrl+Alt+Shift+Q`，永不注销）。
- 注册必须在 UI 线程（`WM_HOTKEY` 投递到注册线程）。前端修改走 `postCmd(cmdSetHotkey)` + channel 回传结果。
- 退出热键允许设成与兜底相同（兜底承担，跳过注册）；开关热键不允许。
- 失败自动回滚旧热键，绝不出现"热键没了程序失联"。

## 构建与测试

```
build.bat           # 编译（-H=windowsgui -s -w）
build.bat dist      # 产出 dist/ 绿色版（exe + README + 空 presets/）
go test ./...       # 回归测试（图标构建、热键解析）
```

- 产物：单文件 exe ~3.5MB，无外部依赖（WebView2 运行时 Win11 自带）。
- `Veil.exe -probe`：遮罩自检 3 秒（验证 WDA 生效）。
- `Veil.exe -settings`：直接打开设置窗（调试）。
- 日志：`%TEMP%\Veil.log`。
- **分发**：走 `build.bat dist`，不要把开发者自己的 `config.json` 和私有图带进包。

## 改代码的硬规则（血泪教训，务必遵守）

**这个项目最大的故障来源不是 Win32 API，是"以为改了其实没改"。**

1. **脚本替换必须回验**：用 python `str.replace()` 改代码时，old 串只要差一个空格或空行，替换就**静默失败**——不报错、不提示，编译器也发现不了。历史上因此翻车过多次：图标改了没生效、面板重置逻辑写了没进文件、预设顺序调整无效、缓存分支没改到。
   - **硬性要求**：关键修改一律用 Edit 工具（匹配失败会报错）；确实要用脚本时，**改完立刻 grep 确认新代码在文件里**，再编译。
2. **改完必须验证行为，不能只看编译通过**：编译通过 ≠ 逻辑生效。验证优先级：**日志实证 > 单元测试 > 渲染截图 > 人工确认**。
3. **排查先看日志，不要靠猜**：`%TEMP%\Veil.log` 记录了线程 ID、遮罩开关、预设列表数量、未确认选择丢弃等关键状态。"预设全空"就是靠"日志里没有 presetList 输出"一步定位到 Bind 未生效的。
4. **跨线程 Win32 调用会静默失败**：`DestroyWindow` 只能销毁本线程创建的窗口，`SetWindowLongPtr` 要在创建线程调用。代码里已有 `inUIThread()` 自检 + 自动转发，新增 Win32 调用沿用这个模式。
5. **用户说"先检查、不要改"时，只输出结论和方案**，等明确指令再动手。
6. **不要在用户没点名的地方擅自改设计**：曾自作主张把设置面板设为置顶，结果面板浮在遮罩上，物理屏和远控屏都看得到。

## 设置面板的状态模型（选中 / 生效 / 预览）

| 概念 | 字段 | 含义 |
|---|---|---|
| 选中 | `config.PresetID` / `CustomImage` | 用户点的那张（蓝色边框） |
| 生效 | `config.AppliedPresetID` / `AppliedCustomImage` | 遮罩实际在用的（「正在使用 / 上次使用」徽章） |
| 预览 | `renderOverride`（atomic） | 临时覆盖，2.5 秒后清除 |

- `specForRender()`：**override > applied > selected**，遮罩渲染只认这个顺序
- 点"确定并启用" → `cmdApply`：快照 applied = selected → rebuild（已开）/ enable（未开）
- 打开面板时有未确认的选择 → **丢弃**（`openSettingsWindow` 开头，日志会打印）
- 遮罩开/关后调 `notifySettingsStateChanged()` → 前端 `refreshState()` 增量刷新（徽章文案、按钮置灰），不重建 DOM
- 面板在遮罩**下方**（普通层级）：物理屏被遮罩挡住，远控端因遮罩被捕获排除而能看到并操作

## 当前状态与待办

- 二次启动 = 重启语义（通知旧实例退出 → 轮询互斥体 → 新实例接管）。
- 覆盖层方案的固有边界（见 README"与 UU 的区别"章节）：物理屏可见光标、IME 候选窗、右键菜单闪现（150ms 抢顶缓解）。要根治需虚拟显示器驱动，是独立的大工程。
- **已完成**：托盘"查看日志"（explorer /select 定位日志文件）。
- **未完成**：设置面板缺"遮蔽全部显示器"开关——`goSetAllMonitors` binding 已存在但前端没有入口，多屏用户无法切到只遮主屏（当前固定遮蔽全部）。
- **计划中**：设置面板重构为左侧导航分页（常规/快捷键/显示/高级/关于）、透明光标（SetSystemCursor + 恢复机制）、菜单图标开关（代码已注释在 `tray.go` showTrayMenu）、开机自启、多语言。完整清单见 README「后续计划」。
- **明确不做（需换技术路线）**：输入法候选窗 / 右键菜单在物理屏闪现——系统级置顶 UI，覆盖层方案无解，需虚拟显示器驱动（WDK + 签名）。

## 与用户的约定

- 用户偏好简洁直接的中文沟通；UI 改动先渲染自查再交付，纯文案/数值修改不必截图。
- 用户自定义过的值优先于代码默认值：config.json 里 toggle=Ctrl+Alt+X、quit=Ctrl+Alt+C 是用户的实际选择，改默认值时不要覆盖他的 config.json。
- UU 素材图（presets/ 里 15 张）仅供用户本机使用，分发时必须移除。
