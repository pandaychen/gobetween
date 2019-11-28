##  Consul服务发现的配置

```toml
#  # -- consul -- #
#  kind = "consul"
#  consul_host = "localhost:8500"       # (required) Consul host:port
#  consul_service_name = "myservice"    # (required) Service name
#  consul_service_tag = ""              # (optional) Service tag
#  consul_service_passing_only = true   # (optional) Get only services with passing healthchecks
#  consul_service_datacenter = ""       # (optional) Datacenter to use
#
#  consul_auth_username = ""   # (optional) HTTP Basic Auth username
#  consul_auth_password = ""   # (optional) HTTP Basic Auth password
#
#  consul_tls_enabled = true                      # (optional) enable client tls auth
#  consul_tls_cert_path = "/path/to/cert.pem"
#  consul_tls_key_path = "/path/to/key.pem"
#  consul_tls_cacert_path = "/path/to/cacert.pem"
```