# Veil 隐幕

Windows 被控端的防窥隐私屏保：运行后**本机物理屏幕被一张图完全盖住，但远控软件看到的仍是真实桌面**，鼠标键盘照常操作。

替代 UU 远程等软件的付费防窥功能。不需要会员、不需要驱动。提供两个版本：单文件绿色**便携版**（约 3.5MB，免安装免管理员），以及遮挡更彻底的**安装版**（见「两个版本」）。

## 功能特性

- **一键防窥**：物理屏全屏遮罩，远控端照常看到桌面并精准操作
- **隐藏物理屏光标**：遮罩期间本机屏幕看不到鼠标光标，关闭遮罩或程序异常退出后自动恢复
- **自定义屏保图**：支持 JPG / PNG / WEBP（≤2MB；分辨率不设硬限制，低分辨率图会提示"放大略模糊"）
- **内置暗色预设**：碳纹 / 极光 / 曜石三张代码生成的背景
- **目录预设**：把图片放进 `presets\` 文件夹即自动加入预设网格
- **三组可自定义热键**：开关防窥 / 打开设置 / 退出程序（录制式修改，冲突自动回滚）
- **固定兜底退出键**：无论热键怎么改，`Ctrl+Alt+Shift+Q` 永远能强制退出
- **多显示器支持**：默认遮蔽全部显示器，可配置只遮主屏
- **便携版单文件**：无安装、无注册表、无后台服务
- **安装版**：遮罩压住输入法候选窗和右键菜单

## 两个版本

| | 便携版 | 安装版 |
|---|---|---|
| 使用方式 | 解压即用，任意目录 | 安装到 `C:\Program Files\Veil`，需一次管理员确认 |
| 物理屏光标 | 隐藏 | 隐藏 |
| 输入法候选窗 | 显示在遮罩之上 | 被遮罩盖住 |
| 右键菜单 | 短暂闪现 | 被遮罩盖住 |
| UAC 弹窗、锁屏 | 盖不住 | 盖不住 |
| 配置与 `presets\` 位置 | exe 同目录 | `%APPDATA%\Veil` |
| 日志 | `%TEMP%\Veil.log` | `%APPDATA%\Veil\Veil.log` |
| 获取方式 | Release 的 `Veil-portable.zip` | Release 的 `Veil-Setup.exe` |

安装版依靠 Windows 的 UIAccess 机制让遮罩进入更高的窗口层级。系统要求这类程序必须带受信任的数字签名并位于 Program Files，所以它无法做成绿色版。

### 安装版会对系统做什么

本项目没有购买代码签名证书，安装包本身未签名，运行时 Windows 会提示"未知发布者"。安装过程（需管理员确认一次）会：

1. 把程序装到 `C:\Program Files\Veil`
2. 在**你的电脑上**现场生成一张自签名证书（`Veil Local Signing`），用它给 `Veil.exe` 签名
3. 把这张证书加入本机"受信任的根证书颁发机构"
4. **立即删除证书私钥**——之后任何人（包括本项目）都无法再用这张证书签别的程序

卸载时证书一并移除。每台电脑的证书都是各自生成的，互不相同。下载后可用 Release 里的 `SHA256SUMS.txt` 校验文件。

已知现象：刚开启遮罩后，UU 远控端的鼠标光标会消失约 10 秒再恢复，之后正常。

## 环境要求

- Windows 10 2004（build 19041）及以上（`WDA_EXCLUDEFROMCAPTURE` 依赖）
- WebView2 运行时（仅设置面板使用，Win11 自带）

## 快速开始

1. 双击 `Veil.exe`，托盘出现图标（启动不自动遮屏，底部有提示条确认运行）
2. `Ctrl+Alt+X` 开启防窥（默认热键，可改）；本机屏幕被盖住，远控端照常操作
3. 再按一次热键关闭；`Ctrl+Alt+C` 退出程序；`Ctrl+Alt+Shift+Q` 强制退出（兜底）

托盘交互：**左键单击 = 打开设置面板**，右键 = 菜单。屏幕被遮住找不到托盘时，**再双击一次 `Veil.exe`**——旧实例会退出（遮罩关闭），新实例自动接管。

## 热键

| 组合 | 作用 | 可自定义 |
|---|---|---|
| `Ctrl+Alt+X` | 开启 / 关闭隐私屏保 | ✅ |
| `Ctrl+Alt+S` | 打开 / 关闭设置面板 | ✅ |
| `Ctrl+Alt+C` | 退出程序 | ✅ |
| `Ctrl+Alt+Shift+Q` | 强制退出（兜底） | ❌ 固定

自定义规则：必须包含 Ctrl 或 Alt；主键限字母 / 数字 / F1–F12；三组不能相同。新组合被占用时自动回滚。

## 设置面板

- **自定义**：上传屏保图（JPG/PNG/WEBP，≤2MB）
- **预设**：内置 3 张暗色背景 + `presets\` 目录里的图片自动加入
- **预览**：用当前选中的图在物理屏试放 2.5 秒，结束后自动恢复生效配置
- **遮罩开启时的面板**：面板显示在遮罩**下方**——物理屏看不到（隐私不中断），远控端能看到并正常操作
- **确定并启用隐私屏保**：保存并立即开启
- **快捷键**：录制式修改三组热键

配置存储在 exe 同目录 `config.json`（便携式，可直接编辑）。

当前版本固定遮蔽**全部显示器**（`config.json` 的 `allMonitors` 字段保留，UI 开关未实现）。

## 工作原理

核心是一行 API：

```go
SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE) // 0x11
```

窗口只出现在物理显示器上，在所有系统采集路径（DXGI 桌面复制、WGC、BitBlt）中被剔除——远控软件采集到的是遮罩**背后**的真实桌面。

配套实现（缺一不可，详见 [AGENTS.md](AGENTS.md)）：

- **鼠标穿透**：`WS_EX_LAYERED | WS_EX_TRANSPARENT` 组合（穿透必须两者齐备），同时立即激活 `SetLayeredWindowAttributes`（LAYERED 窗口不激活就完全不渲染）
- **不抢焦点**：`WS_EX_NOACTIVATE` + `SW_SHOWNOACTIVATE`
- **保顶**：150ms 定时抢回 TOPMOST 并重设捕获排除，对抗其他置顶窗口（安装版另有 UIAccess 层级，不依赖抢顶）
- **隐藏光标**：`SetSystemCursor` 把系统光标换成透明，由看门狗进程保证异常退出后恢复

遮罩窗口是手写的原始 Win32 窗口，不引入任何 GUI 框架。

## 与 UU 远程隐私屏保的区别

UU 基于虚拟显示器驱动：真实桌面被"搬"到虚拟屏，物理屏与桌面分属两条渲染链，因此物理屏上看不到光标、输入法候选窗和右键菜单。

Veil 是覆盖层方案，桌面仍在物理屏上，因此存在固有边界（非 bug）：

- **便携版**：打字时输入法候选窗会显示在遮罩之上；右键菜单会短暂闪现（150ms 抢顶缓解）。安装版没有这两个问题
- **两个版本**：UAC 弹窗和锁屏盖不住；少数程序自己绘制的鼠标光标（部分游戏、设计软件）仍会显示

要彻底消除只能走虚拟显示器驱动路线（独立的大工程）。

## 已知边界

1. **防的是"远控画面"，不是本机操作**：遮罩只挡住物理屏的视觉。坐在电脑前的人虽然看不到内容，但**在这台电脑上按 `Win+Shift+S` 截图，截到的仍是真实桌面**——屏幕上正在发生什么，本机截图都能证明。
2. **系统级界面盖不住**：UAC 提权确认框（管理员弹窗）、Windows 锁屏界面由系统最高层级渲染，会直接显示在遮罩之上。
3. **需要 Windows 10 2004（build 19041）及以上**：更早的系统捕获排除 API 会退化，导致远控端也看到遮罩。
4. 少数显卡驱动（多平面叠加 MPO）下遮罩可能偶发漏进远控画面，属驱动问题。
5. 只做隐私屏保，不做"关闭显示器"（后者是 DDC/CI 硬件控制，另一套技术）。

## 后续计划

- [ ] **设置面板改为左侧导航分页**
  常规（屏保图）/ 快捷键 / 显示（遮蔽范围、菜单图标）/ 高级（开机自启、语言、日志）/ 关于。
  现有单页滚动结构拆成侧栏 + 内容区，前端按页渲染，后续设置项统一挂在这里。

- [ ] **遮蔽范围开关：全部显示器 / 仅主屏**
  面板「显示」页加单选项。后端 `goSetAllMonitors` 已就绪，只缺前端入口；改完需接双屏验证重建逻辑。

- [x] **透明光标：遮罩期间隐藏物理屏鼠标光标**

- [x] **输入法候选窗、右键菜单**：安装版（UIAccess）已解决

- [ ] **排查：开启遮罩后 UU 远控端光标消失约 10 秒**
  首次开启后远控端光标消失约 10 秒再自动恢复，之后正常。Veil 侧无对应事件，疑为 UU 内部切换光标来源的延迟。

- [ ] **设置面板加入"隐藏光标"开关**
  目前只能在 `config.json` 里改 `hideCursor`。

- [ ] **菜单图标开关**
  控制托盘菜单是否显示 `assets/menu/eye.png`。绘制代码已就绪（`tray.go` 的 `makeEyeIcon`，当前注释未挂载），加 config 字段控制即可。

- [ ] **开机自启**
  写注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，设置页加开关。

- [ ] **多语言**
  当前全部文案硬编码中文，需抽语言资源包。

暂不在计划内（需虚拟显示器驱动，与当前用户态实现是两套体系）：

- [ ] 盖住 UAC 弹窗和锁屏

## 构建

依赖：Go 1.21+（开发环境 Go 1.27）、WebView2 运行时。

```
build.bat           # 编译
build.bat dist      # 编译并产出 dist\ 绿色分发目录
go test ./...       # 回归测试
```

或手动：

```
go build -trimpath -ldflags "-H=windowsgui -s -w" -o Veil.exe .
```

安装包（需 [Inno Setup 6](https://jrsoftware.org/isinfo.php) 与 `rsrc`：`go install github.com/akavel/rsrc@latest`）：

```
powershell -ExecutionPolicy Bypass -File scripts\build-uiaccess.ps1 -NoSign
ISCC /DAppVersion=1.1.0 installer\veil.iss        # 产出 build\installer\Veil-Setup.exe
```

推送 `v*` tag 时 GitHub Actions 会自动构建便携版 zip、安装包和 `SHA256SUMS.txt` 并发布到 Release。

开发调试用的安装版（不经安装包，用开发机上的固定证书）：

```
powershell -ExecutionPolicy Bypass -File scripts\build-uiaccess.ps1    # 构建并用本机自签名证书签名
powershell -ExecutionPolicy Bypass -File scripts\deploy-uiaccess.ps1   # 安装并启动（弹一次 UAC）
powershell -ExecutionPolicy Bypass -File scripts\deploy-uiaccess.ps1 -Uninstall
```

开发用脚本会把开发证书（`Veil Dev Code Signing`）加入本机"受信任的根证书颁发机构"，`-Uninstall` 时移除。它装在 `C:\Program Files\Veil Dev`，与安装包的 `C:\Program Files\Veil` 互不覆盖；两者共用 `%APPDATA%\Veil` 下的配置，且同一时间只能运行一个。

## 项目结构

```
Veil.exe            主程序（单文件绿色版）
presets\            目录预设（放入图片自动加入预设网格）
config.json         用户配置（便携式）
assets\menu\        菜单图标源文件
ui\index.html       设置面板 UI（编译时嵌入）
installer\          安装包脚本（Inno Setup）与安装时签名脚本
scripts\            安装版的构建脚本、开发用部署脚本
tools\              图标生成工具（genlogo / genmenu）、窗口层级排查（bandscan）
AGENTS.md           开发者 / Agent 迭代指南（含完整踩坑记录）
```

各源文件职责见 [AGENTS.md](AGENTS.md)。

## 说明

- 本项目仅供个人学习与自用；`presets\` 中的第三方素材图版权归原方，分发时请自行处理
- 迭代开发请先阅读 [AGENTS.md](AGENTS.md)，其中有全部已踩坑记录与设计决策
