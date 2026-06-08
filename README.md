# Onelap-Sync-Strava

让顽鹿运动重新同步 Strava 的小工具。

## 为什么做这个项目？

很多骑友习惯把运动记录都存在 Strava 上。但从 2026 年 3 月 19 日起，顽鹿官方关停了直连 Strava 的通道。本项目就是为了重新打通这个环节：下载顽鹿的 FIT 文件，再通过 Strava 网页上传入口把骑行记录搬到 Strava。


## 它能帮你做什么

- **安全登录**：基于原厂 MD5 签名流程，保障账号安全。
- **聪明地同步**：自动找出还没上传过的记录，不漏传，也不重传。
- **原始 FIT 数据**：直接抓取最完整的 FIT 文件并上传，骑行细节一丁点都不丢失。
- **坐标系修正**：可选地把确认存在偏移的 GCJ-02 坐标转换为 WGS-84，避免 Strava 上轨迹偏移。
- **Strava 网页上传**：默认使用 `https://www.strava.com/upload/select` 的网页登录会话上传 FIT，不依赖 API 上传权限。
- **API 兼容模式**：如果你仍有 Strava API 上传权限，可显式切回 OAuth API 上传，并继续使用 commute/trainer/name/description。
- **灵活的操作方式**：不论是一键全自动同步，还是手动的环境检测，都能找对应的子命令。

## 快速上手

只需三分钟，通过以下五步即可找回自动同步的快乐：

1. **准备配置文件**。把项目根目录下的 `config.sample.json` 复制一份，改名为 `config.json`。
2. **填入账号信息**。打开 `config.json`，填入你的顽鹿账号密码。
3. **准备 Strava 网页 Cookie**。在浏览器里登录 Strava。如果当前登录方式要求邮箱验证码，就先输入邮箱，去邮箱收验证码，填回 Strava 完成网页登录。登录后打开 `https://www.strava.com/upload/select`，在 DevTools 的 Network 面板里刷新一次上传页，复制任意发往 `www.strava.com` 请求里的 `Cookie: ...` 请求头，写入 `config.json` 的 `web_cookie_header`。这是登录凭据，不能提交、不能发给别人。
4. **检查网页登录会话**。终端运行 `./OnelapSyncStrava check`，程序会访问 `https://www.strava.com/upload/select` 并提取本次会话的 CSRF token。
5. **起飞**。运行 `./OnelapSyncStrava` 开始同步。以后每次骑完跑一下它就行，甚至可以丢进后台定时任务。

## 前置要求

- 顽鹿账号
- Strava 网页登录 Cookie（直接写入 `web_cookie_header`）
- 可选：Strava API 应用。只有在 `upload_method` 设置为 `api` 时才需要。

## 下载与安装

### 懒人包 (推荐)

直接去 [GitHub Releases](https://github.com/kermit-r-wood/OnelapSyncStrava/releases) 下载官方构建好的二进制文件即可，支持 Windows, macOS 和 Linux。

### 源码编译

如果你的机器上有 Go 1.21+ 环境，也可以自己动手：

```bash
git clone https://github.com/kermit-r-wood/OnelapSyncStrava.git
cd OnelapSyncStrava
make build
# 或者直接编译
go build -o OnelapSyncStrava main.go
```

## 配置指南

把项目里的 `config.sample.json` 复制一份并改名为 `config.json`，把你的凭证填进去：

```json
{
  "onelap": {
    "account": "你的顽鹿账号",
    "password": "你的顽鹿密码"
  },
    "strava": {
    "upload_method": "web",
    "web_cookie_header": "Cookie: YOUR_STRAVA_COOKIE_HEADER",
    "client_id": "",
    "client_secret": "",
    "access_token": "",
    "refresh_token": "",
    "expires_at": 0
  },
  "convert_gcj_to_wgs": false
}
```

> **关于 Strava 网页 Cookie**：把完整 `Cookie: ...` 请求头写入 `web_cookie_header` 即可。Cookie 内容等同于网页登录凭据，`config.json` 必须留在本机，不能提交、不能发给别人。也可以临时设置环境变量 `STRAVA_WEB_COOKIE_HEADER="Cookie: ..."` 覆盖配置文件中的 Cookie。CSRF token 不需要写入配置，程序每次都会从 `https://www.strava.com/upload/select` 获取新的 token。

> **Cookie 过期预期**：Web Cookie 和 API refresh token 不同，程序不会也不能自动续期 Cookie。Cookie 可能随浏览器会话、Strava 风控、退出登录或 remember token 过期而失效。实际周期不固定，建议预期大约一个月需要重新在浏览器登录并获取一次新的 Cookie；每次同步前运行 `check` 可以提前发现失效。

> **邮箱验证码登录怎么处理**：验证码只用于浏览器网页登录，程序不读取邮箱、不接收验证码、不模拟登录表单。你在浏览器完成验证码登录后，程序只需要后续请求里的 Cookie。Cookie 失效时，重新在浏览器登录并复制新的 `Cookie: ...` 即可。

> **如果还想用官方 API 上传**：把 `upload_method` 改成 `api`，再配置 `client_id` / `client_secret`，运行 `./OnelapSyncStrava auth` 获取 OAuth token。API 模式需要你的 Strava 开发者/API 权限仍可上传。

> **旧配置兼容**：旧版 `config.json` 通常没有 `upload_method` 字段。如果其中已经有 `client_id`、`client_secret` 和 `refresh_token`，程序会自动按旧 API 模式运行；全新配置或没有 OAuth refresh token 的配置才会默认使用 Web 上传。你也可以显式写 `"upload_method": "api"` 或 `"upload_method": "web"`，避免歧义。

> **关于 `convert_gcj_to_wgs`**：默认保持 `false`。大多数 FIT 应直接按原始坐标上传。只有在你确认原始轨迹上传 Strava 后出现整体偏移，并且 GCJ-02 -> WGS-84 后能贴合道路时，才把它设为 `true`。

### 获取 Strava Web Cookie

推荐直接写入 `config.json`，也可以用临时环境变量覆盖：

1. 在浏览器里打开 `https://www.strava.com/login`，按邮箱验证码流程完成登录。
2. 登录后打开 `https://www.strava.com/upload/select`。
3. 打开开发者工具：Chrome/Edge/Firefox 通常按 `F12`，进入 `Network` 面板。
4. 刷新页面，点选一个发往 `www.strava.com` 的请求，例如 `select`。
5. 在请求详情的 `Headers` 里找到 `Request Headers`，复制完整的 `Cookie` 请求头，格式应类似 `Cookie: a=b; c=d`。
6. 写入 `config.json`：

```json
"web_cookie_header": "Cookie: 粘贴完整 Cookie 请求头"
```

也可以只在当前 PowerShell 窗口临时设置：

```powershell
$env:STRAVA_WEB_COOKIE_HEADER = 'Cookie: 粘贴完整 Cookie 请求头'
.\OnelapSyncStrava.exe check
.\OnelapSyncStrava.exe upload-fit downloads\raw.gpx
Remove-Item Env:\STRAVA_WEB_COOKIE_HEADER
```

只需要 Cookie，不需要手动复制 CSRF token。程序每次都会重新访问 `https://www.strava.com/upload/select` 获取新的 CSRF。Cookie 等同于网页登录凭据，不要发给别人，不要提交到仓库。Cookie 不会自动续期，预计可能需要一个月左右重新获取一次；如果 `check` 失败，就按上面的登录流程复制新的 Cookie 并更新 `web_cookie_header`。


## 使用教程

基础命令如下（如果不加参数，程序默认会进 `sync` 同步模式）：
```bash
./OnelapSyncStrava [command]
```

### 1. 测一下连通性 (`check`)

配好之后先别急，跑个 `check` 看看顽鹿和 Strava 都能不能连上：
```bash
./OnelapSyncStrava check
```
这会帮你检查顽鹿账号是否可用，以及 Strava 网页 Cookie 是否还能进入上传页并取得 CSRF token。

### 2. 可选：API 授权 (`auth`)

只有在 `upload_method` 设置为 `api` 时才需要这一步。执行命令后，浏览器会自动弹出来：
```bash
./OnelapSyncStrava auth
```
**关键点**：授权页面记得勾选 **「Upload your activities and posts to Strava」**，否则数据传不上去。
授权搞定后，Token 会自动存进 `config.json`。

*注：如果你在没桌面的远程服务器上跑，可以手动把终端里的链接贴到你本地浏览器访问，完成后把跳转 URL 里的 `code` 贴回给程序就行。*

### 3. 数据同步 (`sync`)

直接运行主程序或者附带 sync 参数，执行数据同步：

```bash
./OnelapSyncStrava sync
```

该模式会：
1. 登录顽鹿
2. 获取当天的骑行活动
3. 判断是否已在 `state.json` 中标记为同步完成，从而过滤重复任务
4. 检查 Strava 网页上传会话，或在 API 模式下刷新 OAuth token
5. 下载对应的 FIT 文件并上传到 Strava
6. 更新本地持久化记录 `state.json`

#### 单文件上传验证 (`upload-fit`)

如果只想验证 Strava 网页上传链路，不想登录顽鹿或改动 `state.json`，可以直接上传一个本地 FIT/GPX/TCX 活动文件：

```bash
./OnelapSyncStrava upload-fit downloads/raw.gpx
```

该命令会使用当前 `strava.upload_method`。默认 Web 模式下，它会先访问 `https://www.strava.com/upload/select` 获取 CSRF token，再把活动文件作为 `files[]` 上传到 Strava 网页上传接口。

看到 `Upload submitted to Strava.` 只表示 Strava 已经接收上传请求，并不保证后台已经解析完成或活动已经创建。验证最终结果时，需要打开 Strava 活动列表或上传处理页面确认对应活动出现。如果活动没有出现，可能是 Strava 后台仍在处理、文件重复、文件格式被拒绝，或登录 Cookie 已失效。

Windows PowerShell 里也可以不把 Cookie 写入配置，直接临时传请求头：

```powershell
$env:STRAVA_WEB_COOKIE_HEADER = 'Cookie: _strava4_session=...; strava_remember_id=...; strava_remember_token=...'
.\OnelapSyncStrava.exe check
.\OnelapSyncStrava.exe upload-fit downloads\raw.gpx
Remove-Item Env:\STRAVA_WEB_COOKIE_HEADER
```

#### 同步历史活动 (`--since`)

默认只同步当天（含昨天）的活动。如果想补传过去某段时间内的活动，加上 `--since` 参数即可：

```bash
# 从指定日期起的所有活动（含 5 月 1 日当天）
./OnelapSyncStrava sync --since=2026-05-01

# 最近 N 天 / 周 / 月 / 年（按本地时区对齐到当天 00:00）
./OnelapSyncStrava sync --since=7d   # 最近 7 天
./OnelapSyncStrava sync --since=2w   # 最近 2 周
./OnelapSyncStrava sync --since=6m   # 最近半年
./OnelapSyncStrava sync --since=1y   # 最近 1 年
```

依然会按 `state.json` 过滤已同步的记录，所以反复跑不会重复上传。

#### 自定义活动标签 / 名称 / 描述

这些选项只适用于 `upload_method: "api"`。默认的 Web 上传流程只负责把 FIT 文件提交给 Strava 网页上传入口，不支持在同一次文件上传请求里设置 commute/trainer/name/description。

```bash
# 把这次同步的活动统一标记为通勤
./OnelapSyncStrava sync -commute

# 室内骑行台
./OnelapSyncStrava sync -trainer

# 自定义名称与描述（注意需要加引号）
./OnelapSyncStrava sync -name="早晨通勤" -description="顽鹿同步"

# 也可以和 -since 等其它 flag 组合
./OnelapSyncStrava sync -since=7d -commute
```

这些字段都是可选的，未指定时 API 上传请求中不会带对应字段，Strava 沿用默认值。

### 4. 查看状态 (`status`)

你可以随时通过此命令快速检查当前环境配置文件和历史同步情况：

```bash
./OnelapSyncStrava status
```
展示当前的账户设定、Strava验证状态，以及历史成功同步的骑行活动条目数。

## 自动后台运行

建议配个定时任务实现“全自动骑完即同步”：
- **Windows**: 丢进 **任务计划程序** 每天跑一两次（或者配合骑行时间）。
- **Linux/macOS**: 配置 `crontab` 定时任务。

## 项目结构

```
OnelapSyncStrava/
├── main.go                     # 项目入口与命令路透
├── config.json                 # 运行配置文件（需手动创建，勿提交）
├── config.sample.json          # 模板配置文件
├── state.json                  # 已同步记录状态（自动生成）
├── Makefile                    # 快捷构建脚本
├── .github/workflows/          # CI/CD 自动发布配置
├── .agents/skills/sync_wizard/ # 针对 Agent 辅助工具的使用指南
├── internal/
│   ├── config/                 # 负责读取配置和记录同步状态 
│   ├── fitconv/                # FIT 文件 GCJ-02 → WGS-84 坐标转换
│   ├── onelap/                 # 顽鹿 API 客户端代码逻辑
│   └── strava/                 # Strava Web 上传、OAuth 与 API 上传实现
├── go.mod
└── go.sum
```

## 许可证

MIT
