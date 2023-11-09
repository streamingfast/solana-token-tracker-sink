package data

import (
	"context"
	"fmt"
	"time"

	pb "github.com/streamingfast/solana-token-tracker-sink/data/pb/solana-token-tracker/v1"
	sink "github.com/streamingfast/substreams-sink"
	pbsubstreamsrpc "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	v1 "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type Sinker struct {
	logger *zap.Logger
	*sink.Sinker
	db            *Psql
	lastClock     *v1.Clock
	blockSecCount int64
}

func NewSinker(logger *zap.Logger, sink *sink.Sinker, db *Psql) *Sinker {
	return &Sinker{
		logger: logger,
		Sinker: sink,
		db:     db,
	}
}

func (s *Sinker) Run(ctx context.Context) error {
	//todo: get cursor
	//var cursor *sink.Cursor

	go func() {
		for {
			time.Sleep(1 * time.Second)
			s.Sinker.AverageBlockSec().Add(s.blockSecCount)
			s.blockSecCount = 0
		}
	}()

	go func() {
		for {
			time.Sleep(5 * time.Second)
			if s.lastClock != nil {
				s.logger.Info("progress_block", zap.Stringer("block", s.lastClock))
			}
			s.logger.Info(s.Sinker.AverageBlockSec().String())
			s.logger.Info(s.Sinker.AverageBlockTimeProcessing().String() + "ms")
		}
	}()

	cursor, err := s.db.FetchCursor()
	if err != nil {
		return fmt.Errorf("fetch cursor: %w", err)
	}
	s.Sinker.Run(ctx, cursor, s)
	return nil
}

func (s *Sinker) HandleBlockScopedData(ctx context.Context, data *pbsubstreamsrpc.BlockScopedData, isLive *bool, cursor *sink.Cursor) (err error) {
	startTime := time.Now()
	s.blockSecCount++
	s.lastClock = data.Clock
	hasTransaction := false

	defer func() {
		s.Sinker.AverageBlockTimeProcessing().Add(time.Since(startTime).Milliseconds())
		if err != nil {
			if hasTransaction {
				e := s.db.RollbackTransaction()
				err = fmt.Errorf("block: %d rollback transaction: %w: while handling err %w", data.Clock.Number, e, err)
			}
			return
		}
		if hasTransaction {
			err = s.db.CommitTransaction()
		}
	}()

	output := data.Output
	if output.Name != s.OutputModuleName() {
		return fmt.Errorf("received data from wrong output module, expected to received from %q but got module's output for %q", s.OutputModuleName(), output.Name)
	}

	if len(output.GetMapOutput().GetValue()) == 0 {
		return nil
	}

	err = s.db.BeginTransaction()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	hasTransaction = true

	moduleOutput := &pb.Output{}
	err = proto.Unmarshal(output.GetMapOutput().GetValue(), moduleOutput)
	if err != nil {
		return fmt.Errorf("unmarshal module output changes: %w", err)
	}

	dbBlockID, err := s.db.HandleClock(data.Clock)
	if err != nil {
		return fmt.Errorf("handle block clock: %w", err)
	}

	err = s.db.HandleInitializedAccount(dbBlockID, moduleOutput.InitializedAccount)
	if err != nil {
		return fmt.Errorf("handle initialized accounts: %w", err)
	}

	if err := s.db.HandleTransfers(dbBlockID, moduleOutput.Transfers); err != nil {
		return fmt.Errorf("handle transfers: %w", err)
	}

	if err := s.db.HandleMints(dbBlockID, moduleOutput.Mints); err != nil {
		return fmt.Errorf("handle mints: %w", err)
	}

	if err := s.db.HandleBurns(dbBlockID, moduleOutput.Burns); err != nil {
		return fmt.Errorf("handle burns: %w", err)
	}

	err = s.db.StoreCursor(cursor)
	if err != nil {
		return fmt.Errorf("store cursor: %w", err)
	}

	return nil
}

func (s *Sinker) HandleBlockUndoSignal(ctx context.Context, undoSignal *pbsubstreamsrpc.BlockUndoSignal, cursor *sink.Cursor) error {
	panic("should not be called on solana")
}
