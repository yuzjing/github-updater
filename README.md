一个用于自动同步 GitHub Actions IP 地址到 nftables 集合（Set）的 Go 工具，确保 CI/CD 流程在严格的防火墙策略下也能顺利进行。

## 场景 (The Problem)

使用 [GitHub Actions](https://github.com/features/actions) 自动部署 [Hugo](https://gohugo.io/) 静态网站到服务器。为了最大限度地保障服务器安全，服务器通常配置 `nftables` 防火墙，并采用了**白名单策略**，默认拒绝所有入站连接，只允许特定 IP 地址访问 SSH 端口。

然而，GitHub Actions 的 Runner（执行构建任务的虚拟机）IP 地址是**动态变化**的，并且分布在一个巨大的地址段中。这就导致了每次 Actions 在执行部署任务（通过 SSH 连接 VPS）时，因为其 IP 不在我的防火墙白名单内而被拦截，从而导致自动化部署流程失败。

手动维护这个庞大且不断变化的 IP 列表是不现实的。

## 解决方案 (The Solution)

为了解决这个问题，我开发了这个 Go 程序。

它作为一个后台服务，通过 `Cron` 定时任务周期性运行。程序会自动从 GitHub 的官方 API 获取最新的 Actions IP 地址列表，并将其与 `nftables` 的一个指定集合（Set）进行同步。

这样，无论 Actions 的 IP 如何变化，它总能被防火墙自动放行，从而保证了自动化部署流程的稳定和安全。

## 主要功能 (Features)

*   **自动获取IP**: 从 GitHub 官方 API (`https://api.github.com/meta`) 获取最新的 Actions IP 地址列表。
*   **智能同步**: 自动比对远端列表和本地 `nftables` 集合的差异，只执行必要的添加和删除操作。
*   **支持 IPv4/IPv6**: 同时处理 GitHub 提供的 IPv4 和 IPv6 地址段。
*   **清理过期IP**: 自动从 `nftables` 集合中移除已不再被 GitHub 使用的旧 IP 地址。
