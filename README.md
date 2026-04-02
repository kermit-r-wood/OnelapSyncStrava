# Onelap-Sync-Strava

让顽鹿运动重新同步 Strava 的小工具。

## 为什么做这个项目？

很多骑友习惯把运动记录都存在 Strava 上。但从 2026 年 3 月 19 日起，顽鹿官方关停了直连 Strava 的通道。本项目就是为了重新打通这个环节：通过 API 自动把你的顽鹿骑行记录搬到 Strava。


## 它能帮你做什么

- **安全登录**：基于原厂 MD5 签名流程，保障账号安全。
- **聪明地同步**：自动找出还没上传过的记录，不漏传，也不重传。
- **原始 FIT 数据**：直接抓取最完整的 FIT 文件并上传，骑行细节一丁点都不丢失。
- **省心的 Strava 授权**：支持 Token 自动刷新。配好以后，它就只是个安静的后台同步工具。
- **灵活的操作方式**：不论是一键全自动同步，还是手动的环境检测，都能找对应的子命令。

## 快速上手

只需三分钟，通过以下五步即可找回自动同步的快乐：

1. **先去 Strava 串个门**。在 [API 设置](https://www.strava.com/settings/api) 里创建一个应用，回调域名（Callback Domain）填 `localhost`，记下生成的 `Client ID` 和 `Secret`。
2. **准备配置文件**。把项目根目录下的 `config.sample.json` 复制一份，改名为 `config.json`。
3. **填入账号信息**。打开 `config.json`，填入你的顽鹿账号密码，以及刚才拿到的那两串 Strava 凭据。
4. **过一遍授权**。终端运行 `./OnelapSyncStrava auth`。浏览器会自动跳出授权页面，你只需要点一下“确认授权”即可。
5. **起飞**。运行 `./OnelapSyncStrava` 开始同步。以后每次骑完跑一下它就行，甚至可以丢进后台定时任务彻底解放双手。

## 前置要求

- 顽鹿账号
- Strava API 应用（[在此创建](https://www.strava.com/settings/api)）
  - **重要配置**：在 Strava 设置页面，将 **Authorization Callback Domain** 设置为 `localhost`。
  - 创建后获取 `Client ID` 和 `Client Secret`。

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
    "client_id": "你的Strava Client ID",
    "client_secret": "你的Strava Client Secret",
    "access_token": "",
    "refresh_token": "",
    "expires_at": 0
  }
}
```

> **常见疑问：我都填了 ID 和 Secret，为什么还要跑 `auth` 命令？**
> 简单来说，ID 和 Secret 是这个“软件”的身份证。而 `auth` 流程是你这位“用户”在亲自点头：我同意把数据授权给它。这步只需要走一次，之后程序拿到 `refresh_token` 就能自动“续命”了。


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
这会帮你检查账户凭据有没有填错，连不连得上 API。

### 2. 握个手授权 (`auth`)

初次运行必须要走这一步。执行命令后，浏览器会自动弹出来：
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
4. 检查并刷新 Strava 令牌（如失效）
5. 下载对应的 FIT 文件并将其上传到 Strava
6. 更新本地持久化记录 `state.json`

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
│   ├── onelap/                 # 顽鹿 API 客户端代码逻辑
│   └── strava/                 # Strava OAuth 与上传交互实现
├── go.mod
└── go.sum
```

## 许可证

MIT
