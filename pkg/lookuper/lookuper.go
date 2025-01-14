package lookuper

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/bee/pkg/feeds"
	"github.com/ethersphere/bee/pkg/feeds/factory"
	"github.com/ethersphere/bee/pkg/soc"
	"github.com/ethersphere/bee/pkg/swarm"
	logger "github.com/ipfs/go-log/v2"
)

var log = logger.Logger("lookuper")

type Lookuper interface {
	Get(ctx context.Context, id string, version int64) (swarm.Address, error)
}

type lookuperImpl struct {
	store   store.PutGetter
	owner   common.Address
	hintMap sync.Map
}

func New(store store.PutGetter, owner common.Address) Lookuper {
	return &lookuperImpl{store: store, owner: owner}
}

func (l *lookuperImpl) Get(ctx context.Context, id string, version int64) (swarm.Address, error) {
	lk, err := factory.New(l.store).NewLookup(feeds.Sequence, feeds.New([]byte(id), l.owner))
	if err != nil {
		return swarm.ZeroAddress, fmt.Errorf("failed creating lookuper %w", err)
	}

	ch, current, _, err := lk.At(ctx, version, l.hint(id))
	if err != nil {
		return swarm.ZeroAddress, fmt.Errorf("failed looking up key %w", err)
	}
	if ch == nil {
		return swarm.ZeroAddress, errors.New("invalid chunk lookup")
	}

	ref, ts, err := ParseFeedUpdate(ch)
	if err != nil {
		return swarm.ZeroAddress, fmt.Errorf("failed parsing feed update %w", err)
	}

	l.setHint(id, current)
	log.Debugf("lookup complete id %s version %d found %d ref %s", id, version, ts, ref.String())

	return ref, nil
}

func (l *lookuperImpl) hint(id string) int64 {
	h, ok := l.hintMap.Load(id)
	if !ok {
		return 0
	}
	return h.(int64)
}

func (l *lookuperImpl) setHint(id string, index feeds.Index) {
	buf, err := index.MarshalBinary()
	if err == nil {
		hint := binary.BigEndian.Uint64(buf)
		l.hintMap.Store(id, int64(hint))
	}
}

func ParseFeedUpdate(ch swarm.Chunk) (swarm.Address, int64, error) {
	s, err := soc.FromChunk(ch)
	if err != nil {
		return swarm.ZeroAddress, 0, fmt.Errorf("soc unmarshal: %w", err)
	}

	update := s.WrappedChunk().Data()
	// split the timestamp and reference
	// possible values right now:
	// unencrypted ref: span+timestamp+ref => 8+8+32=48
	// encrypted ref: span+timestamp+ref+decryptKey => 8+8+64=80
	if len(update) != 48 && len(update) != 80 {
		return swarm.ZeroAddress, 0, fmt.Errorf("invalid update")
	}
	ts := binary.BigEndian.Uint64(update[8:16])
	ref := swarm.NewAddress(update[16:])
	return ref, int64(ts), nil
}
