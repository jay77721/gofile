# BENCHMARKS — 基准测试报告

> 所有数字均为**本仓库可复现的真实测量**,方法与环境见下文。
> 复现:`go test ./util/ -bench . -benchmem -run '^$'`、`go test ./storage/ -bench . -benchmem -run '^$'`。

## 环境

| 项 | 值 |
|----|-----|
| CPU | 16 核 x86_64(容器内 `-16` 后缀) |
| Go | 1.26.4 linux/amd64 |
| MySQL | 8.0.46(Docker,本机 127.0.0.1:3307) |
| 存储 | 本地磁盘(local storage,非 MinIO) |
| 数据量 | 1 用户 1000 个文件记录 |

## 1. 哈希计算(上传/秒传主路径)

| Benchmark | 吞吐 | 分配 |
|-----------|------|------|
| `Sha1` 1MB | **2470 MB/s**(459 µs/op) | 120 B/op, 3 allocs/op |
| `Sha1Stream` 16MB 流式 | 2454 MB/s(7.4 ms/op) | 232 B/op, 4 allocs/op |
| `MD5` 1MB | 1026 MB/s(1.1 ms/op) | 48 B/op, 2 allocs/op |

结论:SHA1 流式计算约 2.5 GB/s,100MB 文件哈希约 40ms,不是上传瓶颈(瓶颈在网络与存储 IO)。

## 2. 本地存储读写

| Benchmark | 吞吐 |
|-----------|------|
| `LocalPut` 1MB | **2856 MB/s**(367 µs/op) |
| `LocalGet` 1MB | **8418 MB/s**(124 µs/op) |

## 3. 端到端:文件列表接口(HTTP + MySQL)

方法:灌入 1000 条 `tbl_file` + `tbl_user_file` 记录,登录后 `curl` 计时(`%{time_total}`),每项 5 次取稳定值。

| 请求 | 延迟 |
|------|------|
| `/file/query?page=1&size=20` | **~2.1 ms**(1.9–2.4 ms) |
| `/file/query?page=50&size=20`(深分页) | 2.1 ms |
| `/file/query?page=1&size=100` | 3.7 ms |
| `/file/query?page=1&size=1000` | 2.9 ms |
| `/file/download`(1MB,本地存储) | 1.5 ms |

结论:查询走 `uk_user_file(user_name, file_sha1)` 唯一索引与 `idx_user_file_status` 索引,深分页无显著退化(LIMIT/OFFSET 代价在 1000 行规模可忽略);1000 条以内列表接口延迟 <5ms。

## 复现步骤

```bash
# 哈希 + 存储基准(无需外部依赖)
go test ./util/ -bench . -benchmem -run '^$'
go test ./storage/ -bench . -benchmem -run '^$'

# 端到端列表基准
docker compose -f docker/docker-compose.yml up -d mysql
# 起服务(见 README),登录后:
#   INSERT 1000 行 tbl_file + tbl_user_file(参考 schema.sql)
curl -s -o /dev/null -w '%{time_total}s\n' -b cookies.txt 'http://localhost:8080/file/query?page=1&size=20'
```

## 已知边界(诚实标注)

- 未做 MinIO 后端与并发(>1 并发)压测;`-race` 构建下数字会略低。
- 无"优化前后"对比:列表接口从第一版就带 `(user_name, file_sha1)` 复合索引,无无索引基线。
- 压测是单机 demo 量级(1000 行),不构成生产容量承诺。
