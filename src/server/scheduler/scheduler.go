package scheduler

/**
 * scheduler.go - schedule operations on backends and manages them
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"fmt"
	"time"

	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/discovery"
	"github.com/yyyar/gobetween/healthcheck"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/metrics"
	"github.com/yyyar/gobetween/stats"
	"github.com/yyyar/gobetween/stats/counters"
)

/**
 * Backend Operation action
 */
type OpAction int

/**
 * Constants for backend operation
 */
const (
	IncrementConnection OpAction = iota
	DecrementConnection
	IncrementRefused
	IncrementTx
	IncrementRx
)

/**
 * Operation on backend
 */
type Op struct {
	target core.Target
	op     OpAction
	param  interface{}
}

/**
 * Request to elect backend
 */
type ElectRequest struct {
	Context  core.Context		//封装了client到lb的连接属性
	Response chan core.Backend	//
	Err      chan error
}

/**
 * Scheduler
 */
type Scheduler struct {

	/* Balancer impl */
	Balancer core.Balancer

	/* Discovery impl */
	Discovery *discovery.Discovery

	/* Healthcheck impl */
	Healthcheck *healthcheck.Healthcheck

	/* ----- backends ------*/

	/* Current cached backends map */
	backends map[core.Target]*core.Backend

	/* Stats */
	StatsHandler *stats.Handler

	/* ----- channels ----- */

	/* Backend operation channel */
	ops chan Op

	/* Stop channel */
	stop chan bool

	/* Elect backend channel */
	elect chan ElectRequest
}

/**
 * Start scheduler
 * 核心函数，gateway
 */
func (this *Scheduler) Start() {

	log := logging.For("scheduler")

	log.Info("Starting scheduler ", this.StatsHandler.Name)

	this.ops = make(chan Op)
	this.elect = make(chan ElectRequest)
	this.stop = make(chan bool)
	this.backends = make(map[core.Target]*core.Backend)

	//服务发现启动（new routine），获取后端列表
	this.Discovery.Start()
	//健康检查启动（new routine）
	this.Healthcheck.Start()

	// backends stats pusher ticker
	backendsPushTicker := time.NewTicker(2 * time.Second)

	/**
	 * Goroutine updates and manages backends
	 * 启动一个独立的groutine
	 */
	go func() {
		for {
			select {

			/* ----- discovery ----- */

			// handle newly discovered backends
			case backends := <-this.Discovery.Discover():
				//接收服务发现获取的后端列表
				this.HandleBackendsUpdate(backends)
				this.Healthcheck.In <- this.Targets()
				this.StatsHandler.BackendsCounter.In <- this.Targets()

			/* ------ healthcheck ----- */

			// handle backend healthcheck result
			case checkResult := <-this.Healthcheck.Out:
				// 从healthy check中获取结果
				this.HandleBackendLiveChange(checkResult.Target, checkResult.Live)

			/* ----- stats ----- */

			// push current backends to stats handler
			case <-backendsPushTicker.C:
				this.StatsHandler.Backends <- this.Backends()

			// handle new bandwidth stats of a backend
			case bs := <-this.StatsHandler.BackendsCounter.Out:
				this.HandleBackendStatsChange(bs.Target, &bs)

			/* ----- operations ----- */

			// handle backend operation
			case op := <-this.ops:
				this.HandleOp(op)

			// elect backend（在TakeBackend函数中触发channel读取）
			case electReq := <-this.elect:
				this.HandleBackendElect(electReq)

			/* ----- stop ----- */

			// handle scheduler stop
			case <-this.stop:
				log.Info("Stopping scheduler ", this.StatsHandler.Name)
				backendsPushTicker.Stop()
				this.Discovery.Stop()
				this.Healthcheck.Stop()
				metrics.RemoveServer(fmt.Sprintf("%s", this.StatsHandler.Name), this.backends)
				return
			}
		}
	}()
}

/**
 * Returns targets of current backends
 */
func (this *Scheduler) Targets() []core.Target {

	keys := make([]core.Target, 0, len(this.backends))
	for k := range this.backends {
		keys = append(keys, k)
	}

	return keys
}

/**
 * Return current backends
 */
func (this *Scheduler) Backends() []core.Backend {

	backends := make([]core.Backend, 0, len(this.backends))
	for _, b := range this.backends {
		backends = append(backends, *b)
	}

	return backends
}

/**
 * Updated backend stats
 */
func (this *Scheduler) HandleBackendStatsChange(target core.Target, bs *counters.BandwidthStats) {

	backend, ok := this.backends[target]
	if !ok {
		logging.For("scheduler").Warn("No backends for checkResult ", target)
		return
	}

	backend.Stats.RxBytes = bs.RxTotal
	backend.Stats.TxBytes = bs.TxTotal
	backend.Stats.RxSecond = bs.RxSecond
	backend.Stats.TxSecond = bs.TxSecond

	metrics.ReportHandleBackendStatsChange(fmt.Sprintf("%s", this.StatsHandler.Name), target, this.backends)
}

/**
 * Updated backend live status
 */
func (this *Scheduler) HandleBackendLiveChange(target core.Target, live bool) {

	backend, ok := this.backends[target]
	if !ok {
		logging.For("scheduler").Warn("No backends for checkResult ", target)
		return
	}

	backend.Stats.Live = live

	metrics.ReportHandleBackendLiveChange(fmt.Sprintf("%s", this.StatsHandler.Name), target, live)
}

/**
 * Update backends map
 */
func (this *Scheduler) HandleBackendsUpdate(backends []core.Backend) {

	// first mark all existing backends as not discovered
	for _, b := range this.backends {
		b.Stats.Discovered = false
	}

	for _, b := range backends {
		oldB, ok := this.backends[b.Target]

		if ok {
			// if we have this backend, update it's discovery properties
			oldB.MergeFrom(b)
			// mark found backend as discovered
			oldB.Stats.Discovered = true
			continue
		}

		b := b // b has to be local variable in order to make unique pointers
		b.Stats.Discovered = true
		this.backends[b.Target] = &b
	}

	//remove not discovered backends without active connections
	for t, b := range this.backends {
		if b.Stats.Discovered || b.Stats.ActiveConnections > 0 {
			continue
		}

		metrics.RemoveBackend(this.StatsHandler.Name, b)

		delete(this.backends, t)
	}
}

/**
 * Perform backend election
 */
//执行选举算法，选出一个后端endpoint节点
func (this *Scheduler) HandleBackendElect(req ElectRequest) {

	// Filter only live and discovered backends
	var backends []*core.Backend
	//this.backends保存了所有的后端endpoint节点
	for _, b := range this.backends {

		if !b.Stats.Live {
			//非健康状态
			continue
		}

		if !b.Stats.Discovered {
			continue
		}

		backends = append(backends, b)
	}

	//初筛后端endpoint
	// Elect backend，从backends中选择一个节点（this.Balancer），传入backends（所有活跃的后端节点）
	backend, err := this.Balancer.Elect(req.Context, backends)
	if err != nil {
		req.Err <- err
		return
	}

	//选出一个，放回channel，触发scheduler的核心loop（这个思路可借鉴）
	req.Response <- *backend
}

/**
 * Handle operation on the backend
 */
func (this *Scheduler) HandleOp(op Op) {

	// Increment global counter, even if
	// backend for this count may be out of discovery pool
	switch op.op {
	case IncrementTx:
		this.StatsHandler.Traffic <- core.ReadWriteCount{CountWrite: op.param.(uint), Target: op.target}
		return
	case IncrementRx:
		this.StatsHandler.Traffic <- core.ReadWriteCount{CountRead: op.param.(uint), Target: op.target}
		return
	}

	log := logging.For("scheduler")

	backend, ok := this.backends[op.target]
	if !ok {
		log.Warn("Trying op ", op.op, " on not tracked target ", op.target)
		return
	}

	switch op.op {
	case IncrementRefused:
		backend.Stats.RefusedConnections++
	case IncrementConnection:
		backend.Stats.ActiveConnections++
		backend.Stats.TotalConnections++
	case DecrementConnection:
		backend.Stats.ActiveConnections--
	default:
		log.Warn("Don't know how to handle op ", op.op)
	}

	metrics.ReportHandleOp(fmt.Sprintf("%s", this.StatsHandler.Name), op.target, this.backends)
}

/**
 * Stop scheduler
 */
func (this *Scheduler) Stop() {
	this.stop <- true
}

/**
 * Take elect backend for proxying
 */
func (this *Scheduler) TakeBackend(context core.Context) (*core.Backend, error) {
	//初始化ElectRequest结构
	r := ElectRequest{context, make(chan core.Backend), make(chan error)}
	// 将r(ElectRequest)结构放入channel，触发channel（this.elect）另一端的逻辑
	this.elect <- r

	//r.Response是一个channel，两个channel，一个error，一个后端channel（非常典型）
	select {
	case err := <-r.Err:
		return nil, err
	case backend := <-r.Response:
		return &backend, nil
	}
}

/**
 * Increment connection refused count for backend
 */
func (this *Scheduler) IncrementRefused(backend core.Backend) {
	this.ops <- Op{backend.Target, IncrementRefused, nil}
}

/**
 * Increment backend connection counter
 */
func (this *Scheduler) IncrementConnection(backend core.Backend) {
	this.ops <- Op{backend.Target, IncrementConnection, nil}
}

/**
 * Decrement backends connection counter
 */
func (this *Scheduler) DecrementConnection(backend core.Backend) {
	this.ops <- Op{backend.Target, DecrementConnection, nil}
}

/**
 * Increment Rx stats for backend
 */
func (this *Scheduler) IncrementRx(backend core.Backend, c uint) {
	this.ops <- Op{backend.Target, IncrementRx, c}
}

/**
 * Increment Tx stats for backends
 */
func (this *Scheduler) IncrementTx(backend core.Backend, c uint) {
	this.ops <- Op{backend.Target, IncrementTx, c}
}
