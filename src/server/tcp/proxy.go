package tcp

/**
 * proxy.go - proxy utils
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"io"
	"net"
	"time"

	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/logging"
)

const (

	/* Buffer size to handle data from socket */
	BUFFER_SIZE = 16 * 1024

	/* Interval of pushing aggregated read/write stats */
	PROXY_STATS_PUSH_INTERVAL = 1 * time.Second
)

/**
 * Perform copy/proxy data from 'from' to 'to' socket, counting r/w stats and
 * dropping connection if timeout exceeded
 */

//典型的proxy封装，可抽象
func proxy(to net.Conn, from net.Conn, timeout time.Duration) <-chan core.ReadWriteCount {

	log := logging.For("proxy")

	stats := make(chan core.ReadWriteCount)
	outStats := make(chan core.ReadWriteCount)

	rwcBuffer := core.ReadWriteCount{}
	ticker := time.NewTicker(PROXY_STATS_PUSH_INTERVAL)
	flushed := false

	// Stats collecting goroutine
	// 这个routine主要用来做状态统计的
	go func() {

		if timeout > 0 {
			from.SetReadDeadline(time.Now().Add(timeout))
		}

		for {
			select {
			case <-ticker.C:
				if !rwcBuffer.IsZero() {
					outStats <- rwcBuffer
				}
				flushed = true
			case rwc, ok := <-stats:

				if !ok {
					ticker.Stop()
					if !flushed && !rwcBuffer.IsZero() {
						outStats <- rwcBuffer
					}
					close(outStats)
					return
				}

				if timeout > 0 && rwc.CountRead > 0 {
					from.SetReadDeadline(time.Now().Add(timeout))
				}

				// Remove non blocking
				if flushed {
					rwcBuffer = rwc
				} else {
					rwcBuffer.CountWrite += rwc.CountWrite
					rwcBuffer.CountRead += rwc.CountRead
				}

				flushed = false
			}
		}
	}()

	// Run proxy copier
	go func() {
		//使用自己封装的Copy函数
		err := Copy(to, from, stats)
		// hack to determine normal close. TODO: fix when it will be exposed in golang
		e, ok := err.(*net.OpError)
		if err != nil && (!ok || e.Err.Error() != "use of closed network connection") {
			log.Warn(err)
		}

		to.Close()
		from.Close()

		// Stop stats collecting goroutine
		close(stats)
	}()

	return outStats
}

/**
 * It's build by analogy of io.Copy
 * 保持io.Copy的功能不变，增加了发送字节统计
 */
func Copy(to io.Writer, from io.Reader, ch chan<- core.ReadWriteCount) error {

	buf := make([]byte, BUFFER_SIZE)
	var err error = nil

	for {
		//从from读数据
		readN, readErr := from.Read(buf)

		if readN > 0 {
			//如果读ok，那么写入to
			writeN, writeErr := to.Write(buf[0:readN])

			if writeN > 0 {
				//增加的逻辑，写入成功时，增加统计数据，通过channel传给统计协程
				ch <- core.ReadWriteCount{CountRead: uint(readN), CountWrite: uint(writeN)}
			}

			if writeErr != nil {
				err = writeErr
				break
			}

			if readN != writeN {
				err = io.ErrShortWrite
				break
			}
		}

		if readErr == io.EOF {
			break
		}

		if readErr != nil {
			err = readErr
			break
		}
	}

	return err
}
