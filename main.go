/**
 * main.go - entry point
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */
package main

import (
	"log"
	"math/rand"
	"os"
	"runtime"
	"time"

	"github.com/yyyar/gobetween/api"
	"github.com/yyyar/gobetween/cmd"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/info"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/manager"
	"github.com/yyyar/gobetween/metrics"
	"github.com/yyyar/gobetween/utils/codec"
)

/**
 * version,revision,branch should be set while build using ldflags (see Makefile)
 */
var (
	version  string
	revision string
	branch   string
)

/**
 * Initialize package
 */
func init() {

	// Set GOMAXPROCS if not set
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(runtime.NumCPU())		//改成uber的cpu库
	}

	// Init random seed
	rand.Seed(time.Now().UnixNano())				//初始化全局种子

	// Save info
	info.Version = version
	info.Revision = revision
	info.Branch = branch
	info.StartTime = time.Now()

}

/**
 * Entry point
 */
func main() {

	log.Printf("gobetween v%s", version)

	env := os.Getenv("GOBETWEEN")
	if env != "" && len(os.Args) > 1 {
		log.Fatal("Passed GOBETWEEN env var and command-line arguments: only one allowed")
	}

	// Try parse env var to args
	if env != "" {
		a := []string{}
		if err := codec.Decode(env, &a, "json"); err != nil {
			log.Fatal("Error converting env var to parameters: ", err, " ", env)
		}
		os.Args = append([]string{""}, a...)
		log.Println("Using parameters from env var: ", os.Args)
	}

	// Process flags and start
	cmd.Execute(func(cfg *config.Config) {

		// Configure logging
		logging.Configure(cfg.Logging.Output, cfg.Logging.Level)

		// Start API
		go api.Start((*cfg).Api)		//开启api功能（gin）

		/* setup metrics */
		go metrics.Start((*cfg).Metrics)		//开启统计功能（Prometheus）

		// Start manager
		go manager.Initialize(*cfg)				//开启核心逻辑

		// block forever					//block here
		<-(chan string)(nil)
	})
}
