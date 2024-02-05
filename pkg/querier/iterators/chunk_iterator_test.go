// SPDX-License-Identifier: AGPL-3.0-only

package iterators

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/stretchr/testify/require"

	"github.com/grafana/mimir/pkg/storage/chunk"
	"github.com/grafana/mimir/pkg/util/test"
)

func TestChunkIteratorAtHistogram(t *testing.T) {
	c := mkChunk(t, 0, 50, 1*time.Millisecond, chunk.PrometheusHistogramChunk)
	it := chunkIterator{Chunk: c, it: c.Data.NewIterator(nil)}
	require.Equal(t, chunkenc.ValHistogram, it.Next())

	ts, firstH := it.AtHistogram(nil)
	require.Equal(t, int64(0), ts)
	require.NotNil(t, firstH)
	test.RequireHistogramEqual(t, test.GenerateTestHistogram(0), firstH)

	it.Next()

	// Verify that after a call to AtHistogram on a non-nil argument,
	// the result and the argument point to the same histogram.
	ts, secondH := it.AtHistogram(firstH)
	require.Equal(t, int64(1), ts)
	require.NotNil(t, secondH)
	test.RequireHistogramEqual(t, test.GenerateTestHistogram(1), secondH)
	require.Equal(t, firstH, secondH)
}

func TestChunkIteratorAtFloatHistogram(t *testing.T) {
	c := mkChunk(t, 0, 50, 1*time.Millisecond, chunk.PrometheusFloatHistogramChunk)
	it := chunkIterator{Chunk: c, it: c.Data.NewIterator(nil)}
	require.Equal(t, chunkenc.ValFloatHistogram, it.Next())

	ts, firstFH := it.AtFloatHistogram(nil)
	require.Equal(t, int64(0), ts)
	require.NotNil(t, firstFH)
	test.RequireFloatHistogramEqual(t, test.GenerateTestFloatHistogram(0), firstFH)

	it.Next()

	// Verify that after a call to AtFloatHistogram on a non-nil argument,
	// the result and the argument point to the same float histogram.
	ts, secondFH := it.AtFloatHistogram(firstFH)
	require.Equal(t, int64(1), ts)
	require.NotNil(t, secondFH)
	test.RequireFloatHistogramEqual(t, test.GenerateTestFloatHistogram(1), secondFH)
	require.Equal(t, firstFH, secondFH)
}

func TestChunkIteratorAtFloatHistogramAfterAtHistogram(t *testing.T) {
	c := mkChunk(t, 0, 50, 1*time.Millisecond, chunk.PrometheusHistogramChunk)
	loadCount := 0
	it := chunkIterator{Chunk: c, it: countAtChunkIterator{it: c.Data.NewIterator(nil), loadCount: &loadCount}}
	require.Equal(t, chunkenc.ValHistogram, it.Next())
	// load histogram and populate cache
	_, h := it.AtHistogram(nil)
	require.NotNil(t, h)
	require.Equal(t, 1, loadCount)
	// read float histogram by converting the cached histogram into a float histogram
	_, fh := it.AtFloatHistogram(nil)
	require.NotNil(t, fh)
	// ensure the resulting float histogram has not been loaded
	require.Equal(t, 1, loadCount)
	// ensure the resulting float histogram is built from the cached histogram
	test.RequireFloatHistogramEqual(t, h.ToFloat(nil), fh)
}

func TestChunkIteratorCaching(t *testing.T) {
	testCases := map[string]struct {
		encoding     chunk.Encoding
		expectedType chunkenc.ValueType
		verifySample func(t *testing.T, i int64, iter *chunkIterator)
	}{
		"float chunk": {
			encoding:     chunk.PrometheusXorChunk,
			expectedType: chunkenc.ValFloat,
			verifySample: func(t *testing.T, i int64, iter *chunkIterator) {
				ts, v := iter.At()
				require.Equal(t, i, ts)
				require.Equal(t, float64(i), v)
			},
		},
		"histogram chunk": {
			encoding:     chunk.PrometheusHistogramChunk,
			expectedType: chunkenc.ValHistogram,
			verifySample: func(t *testing.T, i int64, iter *chunkIterator) {
				ts, h := iter.AtHistogram(nil)
				require.Equal(t, i, ts)
				test.RequireHistogramEqual(t, test.GenerateTestHistogram(int(i)), h)
				// auto convert
				ts2, fh := iter.AtFloatHistogram(nil)
				require.Equal(t, i, ts2)
				test.RequireFloatHistogramEqual(t, test.GenerateTestHistogram(int(i)).ToFloat(nil), fh)
			},
		},
		"float histogram chunk": {
			encoding:     chunk.PrometheusFloatHistogramChunk,
			expectedType: chunkenc.ValFloatHistogram,
			verifySample: func(t *testing.T, i int64, iter *chunkIterator) {
				ts, fh := iter.AtFloatHistogram(nil)
				require.Equal(t, i, ts)
				test.RequireFloatHistogramEqual(t, test.GenerateTestFloatHistogram(int(i)), fh)
			},
		},
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			loadCount := 0
			c := mkChunk(t, 0, 50, 1*time.Millisecond, data.encoding)
			it := &chunkIterator{Chunk: c, it: countAtChunkIterator{it: c.Data.NewIterator(nil), loadCount: &loadCount}}
			require.Equal(t, data.expectedType, it.Next())
			require.Equal(t, 0, loadCount) // Next does not load any value form the underlying iterator and invalidates cache
			require.False(t, it.cacheValid)
			data.verifySample(t, 0, it)
			require.Equal(t, 1, loadCount) // first At* loads a new value from the underlying iterator
			data.verifySample(t, 0, it)
			require.Equal(t, 1, loadCount) // second At* uses cache
			require.Equal(t, int64(0), it.AtT())
			require.Equal(t, 1, loadCount) // AtT after At* uses cache

			require.Equal(t, data.expectedType, it.Next()) // Next does not load any value form the underlying iterator and invalidates cache
			require.Equal(t, 1, loadCount)
			require.False(t, it.cacheValid)
			require.Equal(t, int64(1), it.AtT())
			require.Equal(t, 1, loadCount) // AtT after Next returns underlying iterator's timestamp, but does not load a new value
			data.verifySample(t, 1, it)
			require.Equal(t, 2, loadCount) // first At* after Next loads a new value from the underlying iterator
			require.Equal(t, int64(1), it.AtT())
			require.Equal(t, 2, loadCount) // AtT after At* uses cache

			require.Equal(t, data.expectedType, it.Seek(20)) // Seek does not load any new value from the underlying iterator and invalidates cache
			require.Equal(t, 2, loadCount)
			require.False(t, it.cacheValid)
			data.verifySample(t, 20, it)
			require.Equal(t, 3, loadCount) // first At* after Seek loads a new value from the underlying iterator
			require.Equal(t, int64(20), it.AtT())
			require.Equal(t, 3, loadCount) // AtT after At* uses cache

			require.Equal(t, data.expectedType, it.Seek(30)) // Seek does not load any new value from the underlying iterator and invalidates cache
			require.Equal(t, 3, loadCount)
			require.False(t, it.cacheValid)
			require.Equal(t, int64(30), it.AtT())
			require.Equal(t, 3, loadCount) // AtT after Seek returns underlying iterator's timestamp, but does not load a new value
			data.verifySample(t, 30, it)
			require.Equal(t, 4, loadCount) // first At* after Seek loads a new value from the underlying iterator
			require.Equal(t, int64(30), it.AtT())
			require.Equal(t, 4, loadCount) // AtT after At* uses cache
		})
	}
}

func BenchmarkChunkIterator_AtT(b *testing.B) {
	testCases := map[string]chunk.Encoding{
		"float chunk":           chunk.PrometheusXorChunk,
		"histogram chunk":       chunk.PrometheusHistogramChunk,
		"float histogram chunk": chunk.PrometheusFloatHistogramChunk,
	}
	for testName, encoding := range testCases {
		for _, shouldLoadValue := range []bool{true, false} {
			c := mkChunk(b, 0, 50, 1*time.Millisecond, encoding)
			loadCount := 0
			it := &chunkIterator{Chunk: c, it: countAtChunkIterator{it: c.Data.NewIterator(nil), loadCount: &loadCount}}
			b.Run(fmt.Sprintf("%s-load-value-%v", testName, shouldLoadValue), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					require.NotEqual(b, chunkenc.ValNone, it.Seek(0))
					for valType := it.Next(); valType != chunkenc.ValNone; valType = it.Next() {
						ts1 := atT(it, shouldLoadValue)
						ts2 := atT(it, shouldLoadValue)
						ts3 := it.it.Timestamp()
						require.Equal(b, ts1, ts2)
						require.Equal(b, ts1, ts3)
					}
				}
			})
		}
	}
}

func atT(it *chunkIterator, shouldLoadValue bool) int64 {
	if !shouldLoadValue {
		return it.AtT()
	}
	if it.cacheValid {
		return it.cachedTime
	}
	switch it.valType {
	case chunkenc.ValFloat:
		t, _ := it.At()
		return t
	case chunkenc.ValHistogram:
		t, _ := it.AtHistogram(nil)
		return t
	case chunkenc.ValFloatHistogram:
		t, _ := it.AtFloatHistogram(nil)
		return t
	default:
		panic(fmt.Errorf("chunkIterator: calling AtT with unknown chunk encoding %v", it.valType))
	}
}

type countAtChunkIterator struct {
	it        chunk.Iterator
	loadCount *int
}

func (i countAtChunkIterator) Value() model.SamplePair {
	*i.loadCount++
	return i.it.Value()
}

func (i countAtChunkIterator) AtHistogram(h *histogram.Histogram) (int64, *histogram.Histogram) {
	*i.loadCount++
	return i.it.AtHistogram(h)
}

func (i countAtChunkIterator) AtFloatHistogram(fh *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	*i.loadCount++
	return i.it.AtFloatHistogram(fh)
}

func (i countAtChunkIterator) Batch(size int, valueType chunkenc.ValueType) chunk.Batch {
	return i.it.Batch(size, valueType)
}

func (i countAtChunkIterator) Err() error {
	return i.it.Err()
}

func (i countAtChunkIterator) FindAtOrAfter(t model.Time) chunkenc.ValueType {
	return i.it.FindAtOrAfter(t)
}

func (i countAtChunkIterator) Scan() chunkenc.ValueType {
	return i.it.Scan()
}

func (i countAtChunkIterator) Timestamp() int64 {
	return i.it.Timestamp()
}

func TestChunkIterator_ScanShortcut(t *testing.T) {
	encChk, err := chunk.NewForEncoding(chunk.PrometheusXorChunk)
	require.NoError(t, err)

	for i := 0; i < 120; i++ {
		overflow, err := encChk.Add(model.SamplePair{
			Timestamp: model.Time(i),
			Value:     model.SampleValue(i),
		})
		require.NoError(t, err)
		require.Nil(t, overflow)
	}

	chk := chunk.NewChunk(labels.FromStrings(labels.MetricName, "foobar"), encChk, 0, 119)

	it := chunkIterator{Chunk: chk, it: chk.Data.NewIterator(nil)}

	// Seek past what's in the chunk; triggers the shortcut in seek and returns chunkenc.ValNone.
	valType := it.Seek(120)
	require.Equal(t, chunkenc.ValNone, valType)

	// The iterator is exhausted so it returns chunkenc.ValNone.
	valType = it.Next()
	require.Equal(t, chunkenc.ValNone, valType)

	// Likewise for seeking.
	valType = it.Seek(100)
	require.Equal(t, chunkenc.ValNone, valType)
}
