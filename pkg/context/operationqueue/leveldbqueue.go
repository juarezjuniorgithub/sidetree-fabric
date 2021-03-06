/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package operationqueue

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"os"
	"path"
	"sync"

	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/trustbloc/sidetree-core-go/pkg/api/batch"
)

var (
	errClosed    = errors.New("queue is closed")
	errNotClosed = errors.New("queue must be closed before it can be dropped")
)

type dbHandle interface {
	Put(key, value []byte, wo *opt.WriteOptions) error
	Delete(key []byte, wo *opt.WriteOptions) error
	NewIterator(slice *util.Range, ro *opt.ReadOptions) iterator.Iterator
	Close() error
}

// LevelDBQueue implements an operation queue that's backed by a LevelDB persistent store
type LevelDBQueue struct {
	channelID string
	namespace string
	dir       string
	db        dbHandle
	head      uint64
	tail      uint64 // Non-inclusive
	mutex     sync.RWMutex
	closed    bool
}

func newLevelDBQueue(channelID, namespace, baseDir string) (*LevelDBQueue, error) {
	dir := path.Join(baseDir, channelID, namespace)

	db, err := openFile(dir)
	if err != nil {
		return nil, err
	}

	it := db.NewIterator(nil, nil)

	var first uint64
	var last uint64

	if it.First() {
		first = toUint64(it.Key())
	}

	if it.Last() {
		last = toUint64(it.Key()) + 1
	}

	logger.Infof("[%s-%s] Initialized LevelDB queue in dir [%s]. New [head:tail]: [%d:%d]", channelID, namespace, dir, first, last)

	return &LevelDBQueue{
		channelID: channelID,
		namespace: namespace,
		dir:       dir,
		db:        db,
		head:      first,
		tail:      last,
		mutex:     sync.RWMutex{},
	}, nil
}

// Close closes the database
func (q *LevelDBQueue) Close() {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.closed {
		// Already closed
		return
	}

	logger.Infof("[%s-%s] Closing queue", q.channelID, q.namespace)

	q.closed = true

	if err := q.db.Close(); err != nil {
		logger.Errorf("[%s-%s] Error closing LevelDB [%s]: %s", q.channelID, q.namespace, q.dir, err)
	}
}

// Drop deletes the database. Note that the queue must be closed before this operation may be performed.
func (q *LevelDBQueue) Drop() error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if !q.closed {
		return errNotClosed
	}

	logger.Warnf("[%s-%s] Dropping DB [%s]", q.channelID, q.namespace, q.dir)

	return os.RemoveAll(q.dir)
}

// Add adds the given operation to the tail of the queue
func (q *LevelDBQueue) Add(op *batch.OperationInfo) (uint, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.closed {
		return 0, errClosed
	}

	b, err := marshal(op)
	if err != nil {
		return uint(q.tail - q.head), err
	}

	if err := q.db.Put(toBytes(q.tail), b, nil); err != nil {
		return uint(q.tail - q.head), err
	}

	q.tail++

	logger.Debugf("[%s-%s] Added operation %s. New head:tail - [%d:%d]", q.channelID, q.namespace, op.UniqueSuffix, q.head, q.tail)

	return uint(q.tail - q.head), nil
}

// Remove removes the given number of operation from the head of the queue. The operation are returned
// along with the new size of the queue and any error.
func (q *LevelDBQueue) Remove(num uint) ([]*batch.OperationInfo, uint, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.closed {
		return nil, 0, errClosed
	}

	size := q.tail - q.head

	if size == 0 {
		return nil, 0, nil
	}

	to := q.head + min(uint64(num), size)

	logger.Debugf("[%s-%s] Removing operations in range [%d:%d]", q.channelID, q.namespace, q.head, to-1)

	it := q.db.NewIterator(
		&util.Range{
			Start: toBytes(q.head), // Inclusive
			Limit: toBytes(to),     // Exclusive
		}, nil)

	var ops []*batch.OperationInfo
	var keys [][]byte
	for it.Next() {
		op := &batch.OperationInfo{}
		if err := unmarshal(it.Value(), op); err != nil {
			return nil, 0, err
		}

		logger.Debugf("[%s-%s] Removing operation %s", q.channelID, q.namespace, op.UniqueSuffix)

		ops = append(ops, op)
		keys = append(keys, it.Key())
	}

	// Delete the items
	for i, key := range keys {
		if err := q.db.Delete(key, nil); err != nil {
			logger.Warnf("[%s-%s] Unable to delete the key for item %s", q.channelID, q.namespace, ops[i].UniqueSuffix)
			return nil, 0, errors.WithMessagef(err, "unable to delete the key for item %s", ops[i].UniqueSuffix)
		}
	}

	q.head = to

	logger.Debugf("[%s-%s] New head:tail - [%d:%d]", q.channelID, q.namespace, q.head, q.tail)

	return ops, uint(q.tail - q.head), nil
}

// Peek returns the given number of operation at the head of the queue without removing them.
func (q *LevelDBQueue) Peek(num uint) ([]*batch.OperationInfo, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.closed {
		return nil, errClosed
	}

	size := q.tail - q.head

	if size == 0 {
		return nil, nil
	}

	to := q.head + min(uint64(num), size)

	logger.Debugf("[%s-%s] Returning operations in range [%d:%d]", q.channelID, q.namespace, q.head, to-1)

	it := q.db.NewIterator(
		&util.Range{
			Start: toBytes(q.head), // Inclusive
			Limit: toBytes(to),     // Exclusive
		}, nil)

	var ops []*batch.OperationInfo
	for it.Next() {
		op := &batch.OperationInfo{}
		if err := unmarshal(it.Value(), op); err != nil {
			return nil, err
		}

		logger.Debugf("[%s-%s] Returning operation [%s]", q.channelID, q.namespace, op.UniqueSuffix)

		ops = append(ops, op)
	}

	return ops, nil
}

// Len returns the number of operation in the queue
func (q *LevelDBQueue) Len() uint {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	if q.closed {
		logger.Warnf("[%s-%s] Invocation on a closed queue", q.channelID, q.namespace)
		return 0
	}

	return uint(q.tail - q.head)
}

func toUint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

func toBytes(n uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, n)
	return key
}

func min(i, j uint64) uint64 {
	if i < j {
		return i
	}
	return j
}

func marshal(op *batch.OperationInfo) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(op); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func unmarshal(b []byte, op *batch.OperationInfo) error {
	return gob.NewDecoder(bytes.NewBuffer(b)).Decode(op)
}

// openFile may be overridden by unit tests
var openFile = func(dir string) (dbHandle, error) {
	return leveldb.OpenFile(dir, nil)
}
