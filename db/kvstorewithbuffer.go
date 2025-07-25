package db

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/iotexproject/iotex-core/v2/db/batch"
)

type (
	withBuffer interface {
		batch.Snapshot
		SerializeQueue(batch.WriteInfoSerialize, batch.WriteInfoFilter) []byte
		MustPut(string, []byte, []byte)
		MustDelete(string, []byte)
		Size() int
	}

	// KVStoreWithBuffer defines a KVStore with a buffer, which enables snapshot, revert,
	// and transaction with multiple writes
	KVStoreWithBuffer interface {
		KVStore
		withBuffer
	}

	// kvStoreWithBuffer is an implementation of KVStore, which buffers all the changes
	kvStoreWithBuffer struct {
		store  KVStore
		buffer batch.CachedBatch
	}

	// KVStoreFlusher is a wrapper of KVStoreWithBuffer, which has flush api
	KVStoreFlusher interface {
		SerializeQueue() []byte
		Flush() error
		KVStoreWithBuffer() KVStoreWithBuffer
		BaseKVStore() KVStore
	}

	flusher struct {
		kvb             *kvStoreWithBuffer
		serializeFilter batch.WriteInfoFilter
		serialize       batch.WriteInfoSerialize
		flushTranslate  batch.WriteInfoTranslate
	}

	// KVStoreFlusherOption sets option for KVStoreFlusher
	KVStoreFlusherOption func(*flusher) error
)

// SerializeFilterOption sets the filter for serialize write queue
func SerializeFilterOption(filter batch.WriteInfoFilter) KVStoreFlusherOption {
	return func(f *flusher) error {
		if filter == nil {
			return errors.New("filter cannot be nil")
		}
		f.serializeFilter = filter

		return nil
	}
}

// SerializeOption sets the serialize function for write queue
func SerializeOption(wis batch.WriteInfoSerialize) KVStoreFlusherOption {
	return func(f *flusher) error {
		if wis == nil {
			return errors.New("serialize function cannot be nil")
		}
		f.serialize = wis

		return nil
	}
}

// FlushTranslateOption sets the translate for flush
func FlushTranslateOption(wit batch.WriteInfoTranslate) KVStoreFlusherOption {
	return func(f *flusher) error {
		if wit == nil {
			return errors.New("translate cannot be nil")
		}
		f.flushTranslate = wit

		return nil
	}
}

// NewKVStoreFlusher returns kv store flusher
func NewKVStoreFlusher(store KVStore, buffer batch.CachedBatch, opts ...KVStoreFlusherOption) (KVStoreFlusher, error) {
	if store == nil {
		return nil, errors.New("store cannot be nil")
	}
	if buffer == nil {
		return nil, errors.New("buffer cannot be nil")
	}
	f := &flusher{
		kvb: &kvStoreWithBuffer{
			store:  store,
			buffer: buffer,
		},
	}
	for _, opt := range opts {
		if err := opt(f); err != nil {
			return nil, errors.Wrap(err, "failed to apply option")
		}
	}

	return f, nil
}

func (f *flusher) Flush() error {
	if err := f.kvb.store.WriteBatch(f.kvb.buffer.Translate(f.flushTranslate)); err != nil {
		return err
	}

	f.kvb.buffer.Lock()
	f.kvb.buffer.ClearAndUnlock()

	return nil
}

func (f *flusher) SerializeQueue() []byte {
	return f.kvb.SerializeQueue(f.serialize, f.serializeFilter)
}

func (f *flusher) KVStoreWithBuffer() KVStoreWithBuffer {
	return f.kvb
}

func (f *flusher) BaseKVStore() KVStore {
	return f.kvb.store
}

func (kvb *kvStoreWithBuffer) Start(ctx context.Context) error {
	return kvb.store.Start(ctx)
}

func (kvb *kvStoreWithBuffer) Stop(ctx context.Context) error {
	return kvb.store.Stop(ctx)
}

func (kvb *kvStoreWithBuffer) Snapshot() int {
	return kvb.buffer.Snapshot()
}

func (kvb *kvStoreWithBuffer) RevertSnapshot(sid int) error {
	return kvb.buffer.RevertSnapshot(sid)
}

func (kvb *kvStoreWithBuffer) ResetSnapshots() {
	kvb.buffer.ResetSnapshots()
}

func (kvb *kvStoreWithBuffer) SerializeQueue(
	serialize batch.WriteInfoSerialize,
	filter batch.WriteInfoFilter,
) []byte {
	return kvb.buffer.SerializeQueue(serialize, filter)
}

func (kvb *kvStoreWithBuffer) Size() int {
	return kvb.buffer.Size()
}

func (kvb *kvStoreWithBuffer) Get(ns string, key []byte) ([]byte, error) {
	value, err := kvb.buffer.Get(ns, key)
	if errors.Cause(err) == batch.ErrNotExist {
		value, err = kvb.store.Get(ns, key)
	}
	if errors.Cause(err) == batch.ErrAlreadyDeleted {
		err = errors.Wrapf(ErrNotExist, "failed to get key %x in %s, deleted in buffer level", key, ns)
	}
	return value, err
}

func (kvb *kvStoreWithBuffer) Put(ns string, key, value []byte) error {
	kvb.buffer.Put(ns, key, value, fmt.Sprintf("failed to put %x in %s", key, ns))
	return nil
}

func (kvb *kvStoreWithBuffer) MustPut(ns string, key, value []byte) {
	kvb.buffer.Put(ns, key, value, fmt.Sprintf("failed to put %x in %s", key, ns))
}

func (kvb *kvStoreWithBuffer) Delete(ns string, key []byte) error {
	kvb.buffer.Delete(ns, key, fmt.Sprintf("failed to delete %x in %s", key, ns))
	return nil
}

func (kvb *kvStoreWithBuffer) MustDelete(ns string, key []byte) {
	kvb.buffer.Delete(ns, key, fmt.Sprintf("failed to delete %x in %s", key, ns))
}

func (kvb *kvStoreWithBuffer) Filter(ns string, cond Condition, minKey, maxKey []byte) ([][]byte, [][]byte, error) {
	fk, fv, err := kvb.store.Filter(ns, cond, minKey, maxKey)
	if err != nil {
		return fk, fv, err
	}

	// filter the entries in buffer
	checkMin := len(minKey) > 0
	checkMax := len(maxKey) > 0
	for i := 0; i < kvb.buffer.Size(); i++ {
		entry, err := kvb.buffer.Entry(i)
		if err != nil {
			return nil, nil, err
		}
		if entry.Namespace() != ns {
			continue
		}
		k, v := entry.Key(), entry.Value()

		if checkMin && bytes.Compare(k, minKey) == -1 {
			continue
		}
		if checkMax && bytes.Compare(k, maxKey) == 1 {
			continue
		}

		if cond(k, v) {
			switch entry.WriteType() {
			case batch.Put:
				// if DB contains the same key, that should be obsoleted
				for i := range fk {
					if bytes.Equal(fk[i], k) {
						fk = append(fk[:i], fk[i+1:]...)
						fv = append(fv[:i], fv[i+1:]...)
						break
					}
				}
				fk = append(fk, k)
				fv = append(fv, v)
			case batch.Delete:
				for i := range fk {
					if bytes.Equal(fk[i], k) {
						fk = append(fk[:i], fk[i+1:]...)
						fv = append(fv[:i], fv[i+1:]...)
						break
					}
				}
			}
		}
	}
	return fk, fv, nil
}

func (kvb *kvStoreWithBuffer) WriteBatch(b batch.KVStoreBatch) (err error) {
	kvb.buffer.Append(b)
	return nil
}
