// Copyright 2017, Google
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/kurin/blazer/internal/b2assets"
	"github.com/kurin/blazer/x/window"
)

// StatusInfo reports information about a client.
type StatusInfo struct {
	Writers  map[string]*WriterStatus
	Readers  map[string]*ReaderStatus
	Counters map[time.Duration][]Method
}

type Method struct {
	Name     string
	Duration time.Duration
	Status   int
}

type methodCounter struct {
	d time.Duration
	w *window.Window
}

func (mc methodCounter) record(m Method) {
	mc.w.Insert([]Method{m})
}

func (mc methodCounter) retrieve() []Method {
	ms := mc.w.Reduce()
	return ms.([]Method)
}

func newMethodCounter(d, res time.Duration) methodCounter {
	r := func(i, j interface{}) interface{} {
		a, ok := i.([]Method)
		if !ok {
			a = nil
		}
		b, ok := j.([]Method)
		if !ok {
			b = nil
		}
		for _, m := range b {
			a = append(a, m)
		}
		return a
	}
	return methodCounter{
		d: d,
		w: window.New(d, res, r),
	}
}

// WriterStatus reports the status for each writer.
type WriterStatus struct {
	// Progress is a slice of completion ratios.  The index of a ratio is its
	// chunk id less one.
	Progress []float64
}

// ReaderStatus reports the status for each reader.
type ReaderStatus struct {
	// Progress is a slice of completion ratios.  The index of a ratio is its
	// chunk id less one.
	Progress []float64
}

// Status returns information about the current state of the client.
func (c *Client) Status() *StatusInfo {
	c.slock.Lock()
	defer c.slock.Unlock()

	si := &StatusInfo{
		Writers:  make(map[string]*WriterStatus),
		Readers:  make(map[string]*ReaderStatus),
		Counters: make(map[time.Duration][]Method),
	}

	for name, w := range c.sWriters {
		si.Writers[name] = w.status()
	}

	for name, r := range c.sReaders {
		si.Readers[name] = r.status()
	}

	for _, c := range c.sMethods {
		si.Counters[c.d] = c.retrieve()
	}

	return si
}

func (si *StatusInfo) table() map[string]map[string]int {
	r := make(map[string]map[string]int)
	for d, c := range si.Counters {
		for _, m := range c {
			if _, ok := r[m.Name]; !ok {
				r[m.Name] = make(map[string]int)
			}
			r[m.Name][d.String()]++
		}
	}
	return r
}

func (c *Client) addWriter(w *Writer) {
	c.slock.Lock()
	defer c.slock.Unlock()

	if c.sWriters == nil {
		c.sWriters = make(map[string]*Writer)
	}

	c.sWriters[fmt.Sprintf("%s/%s", w.o.b.Name(), w.name)] = w
}

func (c *Client) removeWriter(w *Writer) {
	c.slock.Lock()
	defer c.slock.Unlock()

	if c.sWriters == nil {
		return
	}

	delete(c.sWriters, fmt.Sprintf("%s/%s", w.o.b.Name(), w.name))
}

func (c *Client) addReader(r *Reader) {
	c.slock.Lock()
	defer c.slock.Unlock()

	if c.sReaders == nil {
		c.sReaders = make(map[string]*Reader)
	}

	c.sReaders[fmt.Sprintf("%s/%s", r.o.b.Name(), r.name)] = r
}

func (c *Client) removeReader(r *Reader) {
	c.slock.Lock()
	defer c.slock.Unlock()

	if c.sReaders == nil {
		return
	}

	delete(c.sReaders, fmt.Sprintf("%s/%s", r.o.b.Name(), r.name))
}

var (
	funcMap = template.FuncMap{
		"inc":    func(i int) int { return i + 1 },
		"lookUp": func(m map[string]int, s string) int { return m[s] },
		"pRange": func(i int) string {
			f := float64(i)
			min := int(math.Pow(2, f)) - 1
			max := min + int(math.Pow(2, f))
			return fmt.Sprintf("%v - %v", time.Duration(min)*time.Millisecond, time.Duration(max)*time.Millisecond)
		},
		"methods": func(si *StatusInfo) []string {
			methods := make(map[string]bool)
			for _, ms := range si.Counters {
				for _, m := range ms {
					methods[m.Name] = true
				}
			}
			var names []string
			for name := range methods {
				names = append(names, name)
			}
			sort.Strings(names)
			return names
		},
		"durations": func(si *StatusInfo) []string {
			var ds []time.Duration
			for d := range si.Counters {
				ds = append(ds, d)
			}
			sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
			var r []string
			for _, d := range ds {
				r = append(r, d.String())
			}
			return r
		},
		"table": func(si *StatusInfo) map[string]map[string]int { return si.table() },
	}
	statusTemplate = template.Must(template.New("status").Funcs(funcMap).Parse(string(b2assets.MustAsset("data/status.html"))))
)

// ServeHTTP satisfies the http.Handler interface.  This means that a Client
// can be passed directly to a path via http.Handle (or on a custom ServeMux or
// a custom http.Server).
//
// ServeHTTP serves diagnostic information about the current state of the
// client; essentially everything available from Client.Status()
func (c *Client) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	info := c.Status()
	statusTemplate.Execute(rw, info)
}
