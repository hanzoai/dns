package metrics

import (
	"sync"

	metric "github.com/luxfi/metric"
)

type reg struct {
	sync.RWMutex
	r map[string]metric.Registry
}

func newReg() *reg { return &reg{r: make(map[string]metric.Registry)} }

// update sets the registry if not already there and returns the input. Or it returns
// a previous set value.
func (r *reg) getOrSet(addr string, pr metric.Registry) metric.Registry {
	r.Lock()
	defer r.Unlock()

	if v, ok := r.r[addr]; ok {
		return v
	}

	r.r[addr] = pr
	return pr
}
