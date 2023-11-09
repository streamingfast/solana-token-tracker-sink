package data

const dbCreateTables = `
CREATE TABLE IF NOT EXISTS solana_tokens.cursor (
	name TEXT PRIMARY KEY,
	cursor TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS solana_tokens.blocks (
	id SERIAL PRIMARY KEY,
	number INTEGER NOT NULL,
	hash TEXT NOT NULL,
	timestamp TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS solana_tokens.transactions (
	id SERIAL PRIMARY KEY,
	block_id INTEGER NOT NULL,
	hash TEXT NOT NULL UNIQUE,
	CONSTRAINT fk_block FOREIGN KEY (block_id) REFERENCES solana_tokens.blocks(id)
);

CREATE TABLE IF NOT EXISTS solana_tokens.derived_addresses (
	id SERIAL PRIMARY KEY,
	transaction_id INTEGER NOT NULL,
	address TEXT NOT NULL,
	derivedAddress TEXT NOT NULL UNIQUE,
	CONSTRAINT fk_transaction FOREIGN KEY (transaction_id) REFERENCES solana_tokens.transactions(id)
);


CREATE TABLE IF NOT EXISTS solana_tokens.mints (
	id SERIAL PRIMARY KEY,
	transaction_id INTEGER NOT NULL,
	to_address TEXT NOT NULL,
	amount DECIMAL NOT NULL,
	CONSTRAINT fk_transaction FOREIGN KEY (transaction_id) REFERENCES solana_tokens.transactions(id)
);

CREATE TABLE IF NOT EXISTS solana_tokens.transfers (
	id SERIAL PRIMARY KEY,
	transaction_id INTEGER NOT NULL,
	from_address TEXT NOT NULL,
	to_address TEXT NOT NULL,
	amount DECIMAL NOT NULL,
	CONSTRAINT fk_transaction FOREIGN KEY (transaction_id) REFERENCES solana_tokens.transactions(id)
);

CREATE TABLE IF NOT EXISTS solana_tokens.burns (
	id SERIAL PRIMARY KEY,
	transaction_id INTEGER NOT NULL,
	from_address TEXT NOT NULL,
	amount DECIMAL NOT NULL,
	CONSTRAINT fk_transaction FOREIGN KEY (transaction_id) REFERENCES solana_tokens.transactions(id)
);
`
