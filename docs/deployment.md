# 部署 Shiyi

这份文档讲怎么把 Shiyi 跑起来。日常用法看 [README](../README.md) 就够了，
这里展开的是一些细节场景。

## 三种跑法

| 方式 | 适合谁 | 特点 |
| --- | --- | --- |
| 容器（Docker） | 多数人 | 数据在卷里，升级镜像不丢数据；镜像自带 `rg`。 |
| 本地二进制 | 想直接用文件、不想装 Docker | 数据在 `data_dir` 指向的目录里。 |
| systemd | 长期跑在自托管的机器上 | 二进制 + 一个 unit 文件。 |

## 容器

### 构建与运行

```bash
docker build -t shiyi:latest .

docker run -d \
  --name shiyi \
  --restart unless-stopped \
  -p 127.0.0.1:8787:8787 \
  -v shiyi-data:/data \
  -e SHIYI_API_KEY="$DEEPSEEK_API_KEY" \
  shiyi:latest
```

注意上面把端口绑到了 `127.0.0.1`，只允许本机访问。
要在局域网里给别人用，改成 `-p 8787:8787` 并**务必**加上 token：

```bash
  -e SHIYI_AUTH_TOKEN="$(openssl rand -hex 24)"
```

### 换配置

镜像内置 `/etc/shiyi/config.toml`。要改，挂一份自己的进去：

```bash
docker run -d \
  --name shiyi \
  -p 8787:8787 \
  -v shiyi-data:/data \
  -v /abs/path/config.toml:/etc/shiyi/config.toml:ro \
  -e SHIYI_API_KEY="$DEEPSEEK_API_KEY" \
  shiyi:latest
```

镜像里那份配置的关键点（完整内容见 [`config.docker.toml`](config.docker.toml)）：

- `data_dir = "/data"`，对应数据卷。
- `listen = "0.0.0.0:8787"`，容器内必须监听全部网卡，否则端口映射不通。
- `api_key_env = "SHIYI_API_KEY"`，密钥只从环境变量读。

### 升级

```bash
docker pull shiyi:latest      # 或者 docker build -t shiyi:latest .
docker rm -f shiyi
docker run ...                # 用同样的参数再起一次
```

数据在 `shiyi-data` 卷里，**换容器不换卷，记录就不丢**。

### 备份

数据就是一个目录里的 JSONL 文件，备份就是拷贝：

```bash
docker run --rm -v shiyi-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/shiyi-$(date +%F).tar.gz -C /data .
```

### 排查

```bash
docker logs -f shiyi                       # 看日志
docker inspect --format '{{.State.Health.Status}}' shiyi   # 看健康状态
docker run --rm --entrypoint rg shiyi:latest --version   # 确认 rg 在
```

## 本地二进制

```bash
go build -o shiyi ./cmd/shiyi
```

配置放 `~/.config/shiyi/config.toml`，或用 `SHIYI_CONFIG` 指到别处。
数据目录默认 `~/.local/share/shiyi`。

```bash
./shiyi serve
```

## systemd

```ini
# /etc/systemd/system/shiyi.service
[Unit]
Description=Shiyi personal memory service
After=network-online.target

[Service]
Type=simple
User=shiyi
Group=shiyi
Environment=SHIYI_CONFIG=/etc/shiyi/config.toml
Environment=DEEPSEEK_API_KEY=sk-...
ExecStart=/usr/local/bin/shiyi serve
Restart=on-failure
RestartSec=5

# 数据目录需要可写
StateDirectory=shiyi

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now shiyi
```

密钥写在 `Environment=` 里会出现在 `systemctl show` 的输出中。
更稳妥的做法是放一个 root 可读的 `EnvironmentFile`：

```ini
EnvironmentFile=/etc/shiyi/shiyi.env
```

```bash
sudo install -m 600 -o root -g shiyi /dev/null /etc/shiyi/shiyi.env
echo 'DEEPSEEK_API_KEY=sk-...' | sudo tee -a /etc/shiyi/shiyi.env >/dev/null
```

## 反向代理

Shiyi 自带 `GET /healthz`，可以直接给负载均衡器做探活。

用 nginx 转发到本机监听的服务：

```nginx
server {
    listen 443 ssl;
    server_name shiyi.example.com;

    # 代理时保留 Authorization 头，Bearer 鉴权才有效。
    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

如果前面挂了 TLS 反代，请在服务侧同时设置 `SHIYI_AUTH_TOKEN`。
**不要**把服务直接暴露到公网而不加 token —— 它是单用户设计，没有任何多租户隔离。

## 启动会失败的情况

进程启动时会做校验，任何一项不满足都会直接退出并给出可操作的错误：

| 情况 | 表现 |
| --- | --- |
| API key 对应的环境变量没设或为空 | 退出。容器里表现为容器立刻退出。 |
| `rg` 不在 PATH 里 | 退出。容器镜像已自带 `rg`。 |
| `data_dir` 不存在或不可写 | 退出。 |
| `shard` 不是 `monthly` / `weekly` / `daily` | 退出。 |
| 配置文件里的时长写成了整数（如 `timeout = 20`） | 退出。时长必须是字符串，如 `"20s"`。 |

排查时先看容器日志里的第一条错误信息。

## 备份与迁移

数据没有加密，也没有导出格式 —— 它就是 JSONL。要迁移就拷数据目录：

```bash
tar czf shiyi-backup.tar.gz -C ~/.local/share/shiyi .
```

迁移到另一台机器后解包到目标机器的 `data_dir` 即可。
分片文件名带日期，跨月份拷过去也不会冲突。
