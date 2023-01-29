// Copyright 2016 Eleme. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package backend

import (
	"encoding/json"
	"errors"
	"github.com/zxf0089216/influx-proxy/logs"
	"os"
)

const (
	VERSION = "1.0"
)

var (
	ErrIllegalConfig = errors.New("illegal config")
)

type NodeConfig struct {
	ListenAddr   string
	Zone         string
	Nexts        string
	Interval     int
	IdleTimeout  int
	WriteTracing int
	QueryTracing int
}

type BackendConfig struct {
	URL             string
	DB              string
	BasicAuth       *BasicAuth
	Zone            string
	Interval        int
	Timeout         int
	TimeoutQuery    int
	MaxRowLimit     int
	CheckInterval   int
	RewriteInterval int
	WriteOnly       int
}

type BasicAuth struct {
	Username string
	Password string
}
type FileConfigSource struct {
	node         string
	BACKENDS     map[string]BackendConfig
	KEYMAPS      map[string]map[string][]string
	NODES        map[string]NodeConfig
	DEFAULT_NODE NodeConfig
}

func NewFileConfigSource(cfgfile string, node string) (fcs *FileConfigSource) {
	fcs = &FileConfigSource{
		node: node,
	}
	file, err := os.Open(cfgfile)
	if err != nil {
		logs.Errorf("file load error: %s", fcs.node)
		return
	}
	defer file.Close()
	dec := json.NewDecoder(file)
	err = dec.Decode(fcs)

	return
}

func (fcs *FileConfigSource) LoadNode() (nodecfg NodeConfig, err error) {
	nodecfg = fcs.NODES[fcs.node]
	if nodecfg.ListenAddr == "" {
		nodecfg.ListenAddr = fcs.DEFAULT_NODE.ListenAddr
	}
	logs.Info("node config loaded.")
	return
}

func (fcs *FileConfigSource) LoadBackends() (backends map[string]*BackendConfig, err error) {
	backends = make(map[string]*BackendConfig)
	for name, val := range fcs.BACKENDS {
		cfg := &BackendConfig{
			URL:             val.URL,
			DB:              val.DB,
			Zone:            val.Zone,
			Interval:        val.Interval,
			Timeout:         val.Timeout,
			TimeoutQuery:    val.TimeoutQuery,
			MaxRowLimit:     val.MaxRowLimit,
			CheckInterval:   val.CheckInterval,
			RewriteInterval: val.RewriteInterval,
			WriteOnly:       val.WriteOnly,
			BasicAuth:       val.BasicAuth,
		}
		if cfg.Interval == 0 {
			cfg.Interval = 1000
		}
		if cfg.Timeout == 0 {
			cfg.Timeout = 10000
		}
		if cfg.TimeoutQuery == 0 {
			cfg.TimeoutQuery = 600000
		}
		if cfg.MaxRowLimit == 0 {
			cfg.MaxRowLimit = 10000
		}
		if cfg.CheckInterval == 0 {
			cfg.CheckInterval = 1000
		}
		if cfg.RewriteInterval == 0 {
			cfg.RewriteInterval = 10000
		}
		backends[name] = cfg
	}
	logs.Debugf("%d backends loaded from file.", len(backends))
	return
}

func (fcs *FileConfigSource) LoadMeasurements() (m_map map[string]map[string][]string, err error) {
	m_map = fcs.KEYMAPS
	logs.Debugf("%d measurements loaded from file.", len(m_map))
	return
}
