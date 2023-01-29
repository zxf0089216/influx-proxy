// Copyright 2016 Eleme. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/zxf0089216/influx-proxy/logs"
)

const (
	WRITE_QUEUE = 16
)

type Backends struct {
	*HttpBackend
	fb              *FileBackend
	Interval        int
	RewriteInterval int
	MaxRowLimit     int32

	running          bool
	ticker           *time.Ticker
	ch_write         chan []byte
	buffer           *bytes.Buffer
	ch_timer         <-chan time.Time
	write_counter    int32
	rewriter_running bool
	wg               sync.WaitGroup
}

// maybe ch_timer is not the best way.
// NewBackends 新建一个Backends对象
func NewBackends(cfg *BackendConfig, name string, storedir string) (bs *Backends, err error) {
	bs = &Backends{
		HttpBackend: NewHttpBackend(cfg),
		// FIXME: path...
		Interval:         cfg.Interval,
		RewriteInterval:  cfg.RewriteInterval,
		running:          true,
		ticker:           time.NewTicker(time.Millisecond * time.Duration(cfg.RewriteInterval)),
		ch_write:         make(chan []byte, 16),
		rewriter_running: false,
		MaxRowLimit:      int32(cfg.MaxRowLimit),
	}
	bs.fb, err = NewFileBackend(name, storedir)
	if err != nil {
		return
	}

	go bs.worker()
	return
}

func (bs *Backends) GetDB() (db string) {
	return bs.DB
}

// worker 新建Backends对象时，启动作为守护协程
func (bs *Backends) worker() {
	for bs.running {
		select {
		case p, ok := <-bs.ch_write:
			if !ok {
				// closed
				bs.Flush()
				bs.wg.Wait()
				bs.HttpBackend.Close()
				bs.fb.Close()
				return
			}
			bs.WriteBuffer(p)

		case <-bs.ch_timer:
			bs.Flush()
			if !bs.running {
				bs.wg.Wait()
				bs.HttpBackend.Close()
				bs.fb.Close()
				return
			}

		case <-bs.ticker.C:
			bs.Idle()
		}
	}
}

// Write 把[]byte类型p发送到ch_write管道中
func (bs *Backends) Write(p []byte) (err error) {
	if !bs.running {
		return io.ErrClosedPipe
	}

	bs.ch_write <- p
	return
}

// Close 退出worker，关闭管道
func (bs *Backends) Close() (err error) {
	bs.running = false
	close(bs.ch_write)
	return
}

// WriteBuffer 对象p写进bs.buffer
func (bs *Backends) WriteBuffer(p []byte) {
	bs.write_counter++

	if bs.buffer == nil {
		bs.buffer = &bytes.Buffer{}
	}

	n, err := bs.buffer.Write(p)
	if err != nil {
		logs.Errorf("buffer.Write error: %s\n", err)
		return
	}
	if n != len(p) {
		err = io.ErrShortWrite
		logs.Errorf("ErrShortWrite error: %s\n", err)
		return
	}

	if p[len(p)-1] != '\n' {
		_, err = bs.buffer.Write([]byte{'\n'})
		if err != nil {
			logs.Errorf("error: %s\n", err)
			return
		}
	}

	switch {
	case bs.write_counter >= bs.MaxRowLimit:
		bs.Flush()
	case bs.ch_timer == nil:
		bs.ch_timer = time.After(
			time.Millisecond * time.Duration(bs.Interval))
	}

	return
}

// Flush 清空管道中的数据到备份文件中
func (bs *Backends) Flush() {
	if bs.buffer == nil {
		return
	}

	p := bs.buffer.Bytes()
	bs.buffer = nil
	bs.ch_timer = nil
	bs.write_counter = 0

	if len(p) == 0 {
		return
	}

	// TODO: limitation
	bs.wg.Add(1)
	go func() {
		defer bs.wg.Done()
		var buf bytes.Buffer
		err := Compress(&buf, p)
		if err != nil {
			logs.Errorf("write file error: %s\n", err)
			return
		}

		p = buf.Bytes()

		// maybe blocked here, run in another goroutine
		if bs.HttpBackend.IsActive() {
			err = bs.HttpBackend.WriteCompressed(p)
			switch err {
			case nil:
				return
			case ErrBadRequest:
				logs.Errorf("bad request, drop all data.")
				return
			case ErrNotFound:
				logs.Errorf("bad backend, drop all data.")
				return
			default:
				logs.Errorf("unknown error %s, maybe overloaded.", err)
			}
			logs.Errorf("write http error: %s\n", err)
		}

		err = bs.fb.Write(p)
		if err != nil {
			logs.Errorf("write file error: %s\n", err)
		}
		// don't try to run rewrite loop directly.
		// that need a lock.
	}()

	return
}

// Idle 数据写入influxdb
func (bs *Backends) Idle() {
	if !bs.rewriter_running && bs.fb.IsData() {
		bs.rewriter_running = true
		go bs.RewriteLoop()
	}

	// TODO: report counter
}

// RewriteLoop
func (bs *Backends) RewriteLoop() {
	for bs.fb.IsData() {
		if !bs.running {
			return
		}
		if !bs.HttpBackend.IsActive() {
			time.Sleep(time.Millisecond * time.Duration(bs.RewriteInterval))
			continue
		}
		err := bs.Rewrite()
		if err != nil {
			time.Sleep(time.Millisecond * time.Duration(bs.RewriteInterval))
			continue
		}
	}
	bs.rewriter_running = false
}

func (bs *Backends) Rewrite() (err error) {
	p, err := bs.fb.Read()
	if err != nil {
		return
	}
	if p == nil { // why?
		return
	}

	err = bs.HttpBackend.WriteCompressed(p)

	switch err {
	case nil:
	case ErrBadRequest:
		logs.Errorf("bad request, drop all data.")
		err = nil
	case ErrNotFound:
		logs.Errorf("bad backend, drop all data.")
		err = nil
	default:
		logs.Errorf("unknown error %s, maybe overloaded.", err)

		err = bs.fb.RollbackMeta()
		if err != nil {
			logs.Errorf("rollback meta error: %s\n", err)
		}
		return
	}

	err = bs.fb.UpdateMeta()
	if err != nil {
		logs.Errorf("update meta error: %s\n", err)
		return
	}
	return
}
