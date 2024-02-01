package data

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	pb "github.com/streamingfast/solana-token-tracker-sink/data/pb/solana-token-tracker/v1"
	sink "github.com/streamingfast/substreams-sink"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"go.uber.org/zap"
)

type Psql struct {
	db     *sql.DB
	tx     *sql.Tx
	logger *zap.Logger
}

type PsqlInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	Dbname   string
}

func (i *PsqlInfo) GetPsqlInfo() string {
	psqlInfo := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		i.Host, i.Port, i.User, i.Password, i.Dbname,
	)
	return psqlInfo
}

func NewPostgreSQL(psqlInfo *PsqlInfo, logger *zap.Logger) *Psql {
	db, err := sql.Open("postgres", psqlInfo.GetPsqlInfo())
	if err != nil {
		panic(err)
	}
	return &Psql{
		db:     db,
		logger: logger,
	}
}

func (p *Psql) Init() error {
	_, err := p.db.Exec(dbCreateTables)
	if err != nil {
		return fmt.Errorf("creating fleets table: %w", err)
	}

	return nil
}
func (p *Psql) HandleClock(clock *pbsubstreams.Clock) (dbBlockID int64, err error) {
	row := p.tx.QueryRow("INSERT INTO solana_tokens.blocks (number, hash, timestamp) VALUES ($1, $2, $3) RETURNING id", clock.Number, clock.Id, clock.Timestamp.AsTime())
	err = row.Err()
	if err != nil {
		return 0, fmt.Errorf("inserting clock: %w", err)
	}

	err = row.Scan(&dbBlockID)
	return
}

func (p *Psql) handleTransaction(dbBlockID int64, transactionHash string) (dbTransactionID int64, err error) {
	//todo: create a transaction cache
	rows, err := p.tx.Query("SELECT id FROM solana_tokens.transactions WHERE hash = $1", transactionHash)
	p.logger.Debug("handling transaction", zap.String("trx_hash", transactionHash))
	if err != nil {
		return 0, fmt.Errorf("selecting transaction: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(&dbTransactionID)
		return
	}

	row := p.tx.QueryRow("INSERT INTO solana_tokens.transactions (hash, block_id) VALUES ($1, $2) RETURNING id", transactionHash, dbBlockID)
	err = row.Err()
	if err != nil {
		return 0, fmt.Errorf("inserting transaction: %w", err)
	}

	err = row.Scan(&dbTransactionID)
	return
}

func (p *Psql) HandleInitializedAccount(dbBlockID int64, initializedAccounts []*pb.InitializedAccount) (err error) {
	for _, initializedAccount := range initializedAccounts {
		dbTransactionID, err := p.handleTransaction(dbBlockID, initializedAccount.TrxHash)
		if err != nil {
			return fmt.Errorf("handling transaction: %w", err)
		}
		_, err = p.tx.Exec("INSERT INTO solana_tokens.derived_addresses (transaction_id, address, derivedAddress) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", dbTransactionID, initializedAccount.Owner, initializedAccount.Account)
		if err != nil {
			return fmt.Errorf("trx_hash: %d inserting derived_addresses: %w", dbBlockID, err)
		}
	}
	return nil
}

var NotFound = errors.New("Not found")

func (p *Psql) resolveAddress(derivedAddress string) (string, error) {
	resolvedAddress := ""
	rows, err := p.tx.Query("SELECT address FROM solana_tokens.derived_addresses WHERE derivedAddress = $1", derivedAddress)
	if err != nil {
		return "", fmt.Errorf("selecting derived_addresses: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(&resolvedAddress)
		return resolvedAddress, nil
	}

	return "", NotFound
}

func (p *Psql) HandleBlockUndo(blockId string) error {
	_, err := p.tx.Exec("DELETE CASCADE FROM solana_tokens.blocks WHERE hash = $1", blockId)
	if err != nil {
		return fmt.Errorf("deleting block: %w", err)
	}
	return nil
}

func (p *Psql) HandleTransfers(dbBlockID int64, transfers []*pb.Transfer) error {
	for _, transfer := range transfers {
		dbTransactionID, err := p.handleTransaction(dbBlockID, transfer.TrxHash)
		if err != nil {
			return fmt.Errorf("inserting transaction: %w", err)
		}
		//{"error": "handle BlockScopedData message: rollback transaction: rolling back transaction: driver: bad connection: while handling err handle transfers: inserting transfer: pq: unexpected Parse response 'C'"}
		_, err = p.tx.Exec("INSERT INTO solana_tokens.transfers (transaction_id, from_address, to_address, amount) VALUES ($1, $2, $3, $4)", dbTransactionID, transfer.From, transfer.To, transfer.Amount)
		if err != nil {
			fmt.Println("processing transfer: ", transfer.From, transfer.To, transfer.Amount, transfer.TrxHash)
			return fmt.Errorf("inserting transfer: %w", err)
		}
	}
	return nil
}

func (p *Psql) insertMint(dbTransactionID int64, mint *pb.Mint) (dbMintID int64, err error) {
	row := p.tx.QueryRow("INSERT INTO solana_tokens.mints (transaction_id, to_address, amount) VALUES ($1, $2, $3) RETURNING id", dbTransactionID, mint.To, mint.Amount)
	err = row.Err()
	if err != nil {
		return 0, fmt.Errorf("inserting mint: %w", err)
	}

	err = row.Scan(&dbMintID)
	return
}

func (p *Psql) HandleMints(dbBlockID int64, mints []*pb.Mint) error {
	for _, mint := range mints {
		dbTransactionID, err := p.handleTransaction(dbBlockID, mint.TrxHash)
		if err != nil {
			return fmt.Errorf("inserting transaction: %w", err)
		}

		_, err = p.insertMint(dbTransactionID, mint)
		if err != nil {
			return fmt.Errorf("inserting mint: %w", err)
		}
	}
	return nil
}

func (p *Psql) insertBurns(dbTransactionID int64, burn *pb.Burn) (dbMintID int64, err error) {
	row := p.tx.QueryRow("INSERT INTO solana_tokens.burns (transaction_id, from_address, amount) VALUES ($1, $2, $3) RETURNING id", dbTransactionID, burn.From, burn.Amount)
	err = row.Err()
	if err != nil {
		return 0, fmt.Errorf("inserting burn: %w", err)
	}

	err = row.Scan(&dbMintID)
	return
}

func (p *Psql) HandleBurns(dbBlockID int64, burns []*pb.Burn) error {
	for _, burn := range burns {
		dbTransactionID, err := p.handleTransaction(dbBlockID, burn.TrxHash)
		if err != nil {
			return fmt.Errorf("inserting transaction: %w", err)
		}

		_, err = p.insertBurns(dbTransactionID, burn)
		if err != nil {
			return fmt.Errorf("inserting burn: %w", err)
		}
	}
	return nil
}

func (p *Psql) StoreCursor(cursor *sink.Cursor) error {
	_, err := p.tx.Exec("INSERT INTO solana_tokens.cursor (name, cursor) VALUES ($1, $2) ON CONFLICT (name) DO UPDATE SET cursor = $2", "token_tracker", cursor.String())
	if err != nil {
		return fmt.Errorf("inserting cursor: %w", err)
	}
	return nil
}

func (p *Psql) FetchCursor() (*sink.Cursor, error) {
	rows, err := p.db.Query("SELECT cursor FROM solana_tokens.cursor WHERE name = $1", "token_tracker")
	if err != nil {
		return nil, fmt.Errorf("selecting cursor: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var cursor string
		err = rows.Scan(&cursor)

		return sink.NewCursor(cursor)
	}
	return nil, nil
}

func (p *Psql) BeginTransaction() error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	p.tx = tx
	return nil
}

func (p *Psql) CommitTransaction() error {
	err := p.tx.Commit()
	if err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	p.tx = nil
	return nil
}

func (p *Psql) RollbackTransaction() error {
	err := p.tx.Rollback()
	if err != nil {
		return fmt.Errorf("rolling back transaction: %w", err)
	}

	p.tx = nil
	return nil
}
