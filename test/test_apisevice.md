##  测试cgi状态查询接口

```bash
curl http://127.0.0.1:8888/servers
```
返回
``` json
    "sample": {
        "max_connections": 0,
        "client_idle_timeout": "0",
        "backend_idle_timeout": "0",
        "backend_connection_timeout": "0",
        "bind": "0.0.0.0:3000",
        "protocol": "tcp",
        "balance": "weight",
        "sni": null,
        "tls": null,
        "backends_tls": null,
        "udp": null,
        "access": null,
        "proxy_protocol": null,
        "discovery": {
            "kind": "static",
            "failpolicy": "keeplast",
            "interval": "0",
            "timeout": "0",
            "static_list": [
                "localhost:8000",
                "localhost:8001"
            ]
        },
        "healthcheck": {
            "kind": "none",
            "interval": "0",
            "passes": 1,
            "fails": 1,
            "timeout": "0"
        }
    },
    "udpsample": {
        "max_connections": 0,
        "client_idle_timeout": "0",
        "backend_idle_timeout": "0",
        "backend_connection_timeout": "0",
        "bind": "localhost:4000",
        "protocol": "udp",
        "balance": "weight",
        "sni": null,
        "tls": null,
        "backends_tls": null,
        "udp": {
            "max_requests": 0,
            "max_responses": 1
        },
        "access": null,
        "proxy_protocol": null,
        "discovery": {
            "kind": "static",
            "failpolicy": "keeplast",
            "interval": "0",
            "timeout": "0",
            "static_list": [
                "8.8.8.8:53",
                "8.8.4.4:53",
                "91.239.100.100:53"
            ]
        },
        "healthcheck": {
            "kind": "none",
            "interval": "0",
            "passes": 1,
            "fails": 1,
            "timeout": "0"
        }
    }
}
```

```bash
curl http://127.0.0.1:8888/servers/sample
```

```json
{
    "max_connections": 0,
    "client_idle_timeout": "0",
    "backend_idle_timeout": "0",
    "backend_connection_timeout": "0",
    "bind": "0.0.0.0:3000",
    "protocol": "tcp",
    "balance": "weight",
    "sni": null,
    "tls": null,
    "backends_tls": null,
    "udp": null,
    "access": null,
    "proxy_protocol": null,
    "discovery": {
        "kind": "static",
        "failpolicy": "keeplast",
        "interval": "0",
        "timeout": "0",
        "static_list": [
            "localhost:8000",
            "localhost:8001"
        ]
    },
    "healthcheck": {
        "kind": "none",
        "interval": "0",
        "passes": 1,
        "fails": 1,
        "timeout": "0"
    }
}
```

```bash
curl http://127.0.0.1:8888/servers/sample/stats
```

```json
{
    "active_connections": 0,
    "rx_total": 344,
    "tx_total": 89,
    "rx_second": 0,
    "tx_second": 0,
    "backends": [
        {
            "host": "localhost",
            "port": "8000",
            "priority": 1,
            "weight": 1,
            "stats": {
                "live": true,
                "discovered": true,
                "total_connections": 0,
                "active_connections": 0,
                "refused_connections": 0,
                "rx": 0,
                "tx": 0,
                "rx_second": 0,
                "tx_second": 0
            }
        },
        {
            "host": "localhost",
            "port": "8001",
            "priority": 1,
            "weight": 1,
            "stats": {
                "live": true,
                "discovered": true,
                "total_connections": 1,
                "active_connections": 0,
                "refused_connections": 0,
                "rx": 344,
                "tx": 89,
                "rx_second": 0,
                "tx_second": 0
            }
        }
    ]
}
```