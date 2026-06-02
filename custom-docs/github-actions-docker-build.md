# GitHub Actions 自动构建 Docker 镜像

本项目通过 `.github/workflows/docker-image-dev.yml` 在代码推送时自动构建并发布 Docker 镜像到 GitHub Container Registry（ghcr.io）。

## 镜像地址

```
ghcr.io/shaoyun/new-api
```

## 触发方式

### 1. 自动触发（代码推送）

向任意分支推送代码时自动触发，`nightly` 分支除外（由独立工作流处理）。

### 2. 手动触发

在 GitHub 仓库页面 → **Actions** → **Publish Docker image (dev)** → **Run workflow**，可手动指定镜像 tag。

## 镜像 Tag 规则

| 场景 | 推送的标签 |
|------|-----------|
| 普通推送 | `<分支名>-<短SHA>`、`dev-latest` |
| commit message 含 `[build:v1.2.3]` | `v1.2.3`、`<分支名>-<短SHA>`、`dev-latest` |
| 手动触发并填写 tag | 指定的 tag、`<分支名>-<短SHA>`、`dev-latest` |

**分支名中的 `/` 和 `:` 会自动转换为 `-`**，例如 `feat/my-feature` → `feat--my-feature`。

## 跳过构建

有两种方式可以跳过本次构建：

### A. 跳过整个 CI（GitHub 原生支持）

在 commit message 中加入以下任意关键字，GitHub 会在加载阶段直接拦截，所有工作流均不运行：

```
[skip ci]
[ci skip]
[no ci]
```

示例：
```
git commit -m "chore: update README [skip ci]"
```

### B. 仅跳过 Docker 构建

在 commit message 中加入以下任意关键字，其他工作流（如测试）仍正常运行，只跳过镜像构建：

```
[skip docker]
[skip build]
[no-build]
```

示例：
```
git commit -m "wip: incomplete feature [skip docker]"
```

## 拉取镜像

```bash
# 最新 dev 构建
docker pull ghcr.io/shaoyun/new-api:dev-latest

# 指定分支的最新构建（示例）
docker pull ghcr.io/shaoyun/new-api:main-abc1234

# 指定版本（通过 commit message 或手动触发打的 tag）
docker pull ghcr.io/shaoyun/new-api:v1.2.3
```

## 认证说明

镜像仓库使用 `GITHUB_TOKEN` 自动认证，无需配置额外的 Secret。

拉取私有镜像时需要先登录：

```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin
```
