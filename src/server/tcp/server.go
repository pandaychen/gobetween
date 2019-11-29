package tcp

//负载均衡网关的tcp-loadbalance实现

/**
 * server.go - proxy server implementation
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/yyyar/gobetween/balance"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/discovery"
	"github.com/yyyar/gobetween/healthcheck"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/server/modules/access"
	"github.com/yyyar/gobetween/server/scheduler"
	"github.com/yyyar/gobetween/stats"
	"github.com/yyyar/gobetween/utils"
	"github.com/yyyar/gobetween/utils/proxyprotocol"
	tlsutil "github.com/yyyar/gobetween/utils/tls"
	"github.com/yyyar/gobetween/utils/tls/sni"
)

/**
 * Server listens for client connections and proxies it to backends
 */
//网关的tcp转发bind封装（）
type Server struct {

	/* Server friendly name */
	name string

	/* Listener */
	listener net.Listener

	/* Configuration */
	cfg config.Server

	/* Scheduler deals with discovery, balancing and healthchecks */
	scheduler scheduler.Scheduler	//核心结构体，用于实现服务发现、负载均衡和健康检查的逻辑

	/* Current clients connection */
	clients map[string]net.Conn		//SAVE-当前客户端的连接？

	/* Stats handler */
	statsHandler *stats.Handler		//状态统计

	/* ----- channels ----- */

	/* Channel for new connections */
	connect chan (*core.TcpContext)

	/* Channel for dropping connections or connectons to drop */
	disconnect chan (net.Conn)

	/* Stop channel */
	stop chan bool

	/* Tls config used to connect to backends */
	backendsTlsConfg *tls.Config

	/* Tls config used for incoming connections */
	tlsConfig *tls.Config

	/* Get certificate filled by external service */
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)

	/* ----- modules ----- */

	/* Access module checks if client is allowed to connect */
	// 访问控制配置
	access *access.Access
}

/**
 * Creates new server instance
 */
func New(name string, cfg config.Server) (*Server, error) {

	log := logging.For("server")

	var err error = nil
	statsHandler := stats.NewHandler(name)

	// Create server，初始化server结构
	server := &Server{
		name:         name,
		cfg:          cfg,
		stop:         make(chan bool),
		disconnect:   make(chan net.Conn),
		connect:      make(chan *core.TcpContext),
		clients:      make(map[string]net.Conn),
		statsHandler: statsHandler,
		scheduler: scheduler.Scheduler{
			//调用各自的new()初始化
			Balancer:     balance.New(cfg.Sni, cfg.Balance),
			Discovery:    discovery.New(cfg.Discovery.Kind, *cfg.Discovery),
			Healthcheck:  healthcheck.New(cfg.Healthcheck.Kind, *cfg.Healthcheck),
			StatsHandler: statsHandler,
		},
	}

	/* Add access if needed */
	if cfg.Access != nil {
		server.access, err = access.NewAccess(cfg.Access)
		if err != nil {
			return nil, err
		}
	}

	/* Add tls configs if needed */
	//初始化连接后端配置
	server.backendsTlsConfg, err = tlsutil.MakeBackendTLSConfig(cfg.BackendsTls)
	if err != nil {
		return nil, err
	}

	log.Info("Creating '", name, "': ", cfg.Bind, " ", cfg.Balance, " ", cfg.Discovery.Kind, " ", cfg.Healthcheck.Kind)

	return server, nil
}

/**
 * Returns current server configuration
 */
func (this *Server) Cfg() config.Server {
	return this.cfg
}

/**
 * Start server，启动Server（核心逻辑）
 */
func (this *Server) Start() error {

	var err error
	//本网关的tls配置
	this.tlsConfig, err = tlsutil.MakeTlsConfig(this.cfg.Tls, this.GetCertificate)
	if err != nil {
		return err
	}

	go func() {
		//goroutine runs forever（routine best practice）
		for {
			select {
			case client := <-this.disconnect:
				//处理连接断开事件
				this.HandleClientDisconnect(client)

			case ctx := <-this.connect:
				//处理新连接事件
				this.HandleClientConnect(ctx)

			case <-this.stop:
				//处理停止事件
				this.scheduler.Stop()
				this.statsHandler.Stop()
				if this.listener != nil {
					this.listener.Close()
					for _, conn := range this.clients {
						conn.Close()
					}
				}
				this.clients = make(map[string]net.Conn)
				return
			}
		}
	}()

	// Start stats handler
	this.statsHandler.Start()

	// Start scheduler
	this.scheduler.Start()

	// Start listening
	if err := this.Listen(); err != nil {
		this.Stop()
		return err
	}

	return nil
}

/**
 * Handle client disconnection
 */
func (this *Server) HandleClientDisconnect(client net.Conn) {
	client.Close()
	delete(this.clients, client.RemoteAddr().String())
	this.statsHandler.Connections <- uint(len(this.clients))
}

/**
 * Handle new client connection
 */
func (this *Server) HandleClientConnect(ctx *core.TcpContext) {
	client := ctx.Conn
	log := logging.For("server")

	if *this.cfg.MaxConnections != 0 && len(this.clients) >= *this.cfg.MaxConnections {
		log.Warn("Too many connections to ", this.cfg.Bind)
		client.Close()
		return
	}

	this.clients[client.RemoteAddr().String()] = client
	this.statsHandler.Connections <- uint(len(this.clients))
	go func() {
		this.handle(ctx)
		this.disconnect <- client
	}()
}

/**
 * Stop, dropping all connections
 */
func (this *Server) Stop() {

	log := logging.For("server.Listen")
	log.Info("Stopping ", this.name)

	this.stop <- true
}

//处理新连接的封装函数
func (this *Server) wrap(conn net.Conn, sniEnabled bool) {
	log := logging.For("server.Listen.wrap")

	var hostname string
	var err error

	if sniEnabled {
		var sniConn net.Conn
		sniConn, hostname, err = sni.Sniff(conn, utils.ParseDurationOrDefault(this.cfg.Sni.ReadTimeout, time.Second*2))

		if err != nil {
			log.Error("Failed to get / parse ClientHello for sni: ", err)
			conn.Close()
			return
		}

		conn = sniConn
	}

	if this.tlsConfig != nil {
		conn = tls.Server(conn, this.tlsConfig)
	}

	//向channel放入连接（excited）
	this.connect <- &core.TcpContext{
		hostname,
		conn,
	}

}

/**
 * Listen on specified port for a connections
 */
//TCP-server-bind：网关的tcp监听服务
func (this *Server) Listen() (err error) {

	log := logging.For("server.Listen")

	// create tcp listener，创建listener
	this.listener, err = net.Listen("tcp", this.cfg.Bind)

	if err != nil {
		log.Error("Error starting ", this.cfg.Protocol+" server: ", err)
		return err
	}

	sniEnabled := this.cfg.Sni != nil

	go func() {
		for {
			//接收一个客户端的连接请求
			conn, err := this.listener.Accept()

			if err != nil {
				log.Error(err)
				return
			}

			//处理新连接（异步，一个连接一个routine，不优雅）
			go this.wrap(conn, sniEnabled)
		}
	}()

	return nil
}

/**
 * Handle incoming connection and prox it to backend
 */
func (this *Server) handle(ctx *core.TcpContext) {
	clientConn := ctx.Conn
	log := logging.For("server.handle [" + this.cfg.Bind + "]")

	/* Check access if needed */
	if this.access != nil {
		if !this.access.Allows(&clientConn.RemoteAddr().(*net.TCPAddr).IP) {
			log.Debug("Client disallowed to connect ", clientConn.RemoteAddr())
			clientConn.Close()
			return
		}
	}

	log.Debug("Accepted ", clientConn.RemoteAddr(), " -> ", this.listener.Addr())

	/* Find out backend for proxying */
	var err error
	backend, err := this.scheduler.TakeBackend(ctx)
	if err != nil {
		log.Error(err, "; Closing connection: ", clientConn.RemoteAddr())
		return
	}

	/* Connect to backend */
	var backendConn net.Conn

	if this.cfg.BackendsTls != nil {
		backendConn, err = tls.DialWithDialer(&net.Dialer{
			Timeout: utils.ParseDurationOrDefault(*this.cfg.BackendConnectionTimeout, 0),
		}, "tcp", backend.Address(), this.backendsTlsConfg)

	} else {
		backendConn, err = net.DialTimeout("tcp", backend.Address(), utils.ParseDurationOrDefault(*this.cfg.BackendConnectionTimeout, 0))
	}

	if err != nil {
		this.scheduler.IncrementRefused(*backend)
		log.Error(err)
		return
	}
	this.scheduler.IncrementConnection(*backend)
	defer this.scheduler.DecrementConnection(*backend)

	/* Send proxy protocol header if configured */
	if this.cfg.ProxyProtocol != nil {
		switch this.cfg.ProxyProtocol.Version {
		case "1":
			log.Debug("Sending proxy_protocol v1 header ", clientConn.RemoteAddr(), " -> ", this.listener.Addr(), " -> ", backendConn.RemoteAddr())
			err := proxyprotocol.SendProxyProtocolV1(clientConn, backendConn)
			if err != nil {
				log.Error(err)
				return
			}
		default:
			log.Error("Unsupported proxy_protocol version " + this.cfg.ProxyProtocol.Version + ", aborting connection")
			return
		}
	}

	/* ----- Stat proxying ----- */

	log.Debug("Begin ", clientConn.RemoteAddr(), " -> ", this.listener.Addr(), " -> ", backendConn.RemoteAddr())
	cs := proxy(clientConn, backendConn, utils.ParseDurationOrDefault(*this.cfg.BackendIdleTimeout, 0))
	bs := proxy(backendConn, clientConn, utils.ParseDurationOrDefault(*this.cfg.ClientIdleTimeout, 0))

	isTx, isRx := true, true
	for isTx || isRx {
		select {
		case s, ok := <-cs:
			isRx = ok
			if !ok {
				cs = nil
				continue
			}
			this.scheduler.IncrementRx(*backend, s.CountWrite)
		case s, ok := <-bs:
			isTx = ok
			if !ok {
				bs = nil
				continue
			}
			this.scheduler.IncrementTx(*backend, s.CountWrite)
		}
	}

	log.Debug("End ", clientConn.RemoteAddr(), " -> ", this.listener.Addr(), " -> ", backendConn.RemoteAddr())
}
