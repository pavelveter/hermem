package compression

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	compressionCount     atomic.Int64
	recompressCount      atomic.Int64
	regenerateCount      atomic.Int64
	compressedEntities   atomic.Int64
	clusterCount         atomic.Int64
	clusterSizes         []int64
	totalDurationNS      atomic.Int64
	recompressDurationNS atomic.Int64
	mu                   atomic.Int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		clusterSizes: make([]int64, 0),
	}
}

func (m *Metrics) IncCompress() {
	m.compressionCount.Add(1)
}

func (m *Metrics) IncRecompress() {
	m.recompressCount.Add(1)
}

func (m *Metrics) IncRegenerate() {
	m.regenerateCount.Add(1)
}

func (m *Metrics) AddCompressedEntities(n int) {
	m.compressedEntities.Add(int64(n))
}

func (m *Metrics) AddClusterSize(n int) {
	m.clusterCount.Add(1)
}

func (m *Metrics) ObserveCompressDuration(d time.Duration) {
	m.totalDurationNS.Add(d.Nanoseconds())
}

func (m *Metrics) ObserveRecompressDuration(d time.Duration) {
	m.recompressDurationNS.Add(d.Nanoseconds())
}

func (m *Metrics) CompressCount() int64        { return m.compressionCount.Load() }
func (m *Metrics) RecompressCount() int64      { return m.recompressCount.Load() }
func (m *Metrics) RegenerateCount() int64      { return m.regenerateCount.Load() }
func (m *Metrics) CompressedEntities() int64   { return m.compressedEntities.Load() }
func (m *Metrics) ClusterCount() int64         { return m.clusterCount.Load() }
func (m *Metrics) TotalDuration() time.Duration { return time.Duration(m.totalDurationNS.Load()) }
func (m *Metrics) RecompressDuration() time.Duration { return time.Duration(m.recompressDurationNS.Load()) }
