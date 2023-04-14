DROP TABLE IF EXISTS spendings;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS accounts;

CREATE TABLE accounts (
	id             INT NOT NULL AUTO_INCREMENT,
	email          VARCHAR(64) NOT NULL UNIQUE,
	password_hash  VARCHAR(64) NOT NULL,
	verified       BOOL NOT NULL,
	created        INT NOT NULL,
	nonce          VARCHAR(32) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE balances (
	id         INT NOT NULL AUTO_INCREMENT,
	email      VARCHAR(64) NOT NULL UNIQUE,
	subscribed BOOL NOT NULL,
	balance    DOUBLE NOT NULL,
	locked     DOUBLE NOT NULL,
	currency   VARCHAR(8) NOT NULL,
	stripe_id  VARCHAR(32) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES accounts(email)
);

CREATE TABLE payments (
	id         INT NOT NULL AUTO_INCREMENT,
	email      VARCHAR(64) NOT NULL,
	amount     DOUBLE NOT NULL,
	currency   VARCHAR(8) NOT NULL,
	amount_usd DOUBLE NOT NULL,
	made       INT NOT NULL,
	pending    BOOL NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES accounts(email)
);

CREATE TABLE spendings (
	id               INT NOT NULL AUTO_INCREMENT,
	email            VARCHAR(64) NOT NULL UNIQUE,
	current_locked   DOUBLE NOT NULL,
	current_used     DOUBLE NOT NULL,
	current_overhead DOUBLE NOT NULL,
	prev_locked      DOUBLE NOT NULL,
	prev_used        DOUBLE NOT NULL,
	prev_overhead    DOUBLE NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES accounts(email)
);

DROP TABLE IF EXISTS hosts;
DROP TABLE IF EXISTS scanhistory;
DROP TABLE IF EXISTS ipnets;

CREATE TABLE hosts (
	id                               INT NOT NULL AUTO_INCREMENT,
	accepting_contracts              BOOL NOT NULL,
	max_download_batch_size          BIGINT UNSIGNED NOT NULL,
	max_duration                     BIGINT UNSIGNED NOT NULL,
	max_revise_batch_size            BIGINT UNSIGNED NOT NULL,
	net_address                      VARCHAR(255) NOT NULL,
	remaining_storage                BIGINT UNSIGNED NOT NULL,
	sector_size                      BIGINT UNSIGNED NOT NULL,
	total_storage                    BIGINT UNSIGNED NOT NULL,
	unlock_hash                      VARCHAR(64) NOT NULL,
	window_size                      BIGINT UNSIGNED NOT NULL,
	collateral                       VARCHAR(64) NOT NULL,
	max_collateral                   VARCHAR(64) NOT NULL,
	base_rpc_price                   VARCHAR(64) NOT NULL,
	contract_price                   VARCHAR(64) NOT NULL,
	download_bandwidth_price         VARCHAR(64) NOT NULL,
	sector_access_price              VARCHAR(64) NOT NULL,
	storage_price                    VARCHAR(64) NOT NULL,
	upload_bandwidth_price           VARCHAR(64) NOT NULL,
	ephemeral_account_expiry         BIGINT NOT NULL,
	max_ephemeral_account_balance    VARCHAR(64) NOT NULL,
	revision_number                  BIGINT UNSIGNED NOT NULL,
	version                          VARCHAR(16) NOT NULL,
	sia_mux_port                     VARCHAR(8) NOT NULL,
	first_seen                       BIGINT UNSIGNED NOT NULL,
	historic_downtime                BIGINT NOT NULL,
	historic_uptime                  BIGINT NOT NULL,
	historic_failed_interactions     DOUBLE NOT NULL,
	historic_successful_interactions DOUBLE NOT NULL,
	recent_failed_interactions       DOUBLE NOT NULL,
	recent_successful_interactions   DOUBLE NOT NULL,
	last_historic_update             BIGINT UNSIGNED NOT NULL,
	last_ip_net_change               VARCHAR(64) NOT NULL,
	public_key                       VARCHAR(128) NOT NULL UNIQUE,
	filtered                         BOOL NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE scanhistory (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key VARCHAR(128) NOT NULL,
	time       VARCHAR(64) NOT NULL,
	success    BOOL NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (public_key) REFERENCES hosts(public_key)
);

CREATE TABLE ipnets (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key VARCHAR(128) NOT NULL,
	ip_net     VARCHAR(255) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (public_key) REFERENCES hosts(public_key)
);

DROP TABLE IF EXISTS renters;
DROP TABLE IF EXISTS contracts;
DROP TABLE IF EXISTS transactions;

CREATE TABLE renters (
	id                           INT NOT NULL AUTO_INCREMENT,
	email                        VARCHAR(64) NOT NULL UNIQUE,
	public_key                   VARCHAR(128) NOT NULL UNIQUE,
	current_period               BIGINT UNSIGNED NOT NULL,
	funds                        VARCHAR(64) NOT NULL,
	hosts                        BIGINT UNSIGNED NOT NULL,
	period                       BIGINT UNSIGNED NOT NULL,
	renew_window                 BIGINT UNSIGNED NOT NULL,
	expected_storage             BIGINT UNSIGNED NOT NULL,
	expected_upload              BIGINT UNSIGNED NOT NULL,
	expected_download            BIGINT UNSIGNED NOT NULL,
	min_shards                   BIGINT UNSIGNED NOT NULL,
	total_shards                 BIGINT UNSIGNED NOT NULL,
	max_rpc_price                VARCHAR(64) NOT NULL,
	max_contract_price           VARCHAR(64) NOT NULL,
	max_download_bandwidth_price VARCHAR(64) NOT NULL,
	max_sector_access_price      VARCHAR(64) NOT NULL,
	max_storage_price            VARCHAR(64) NOT NULL,
	max_upload_bandwidth_price   VARCHAR(64) NOT NULL,
	min_max_collateral           VARCHAR(64) NOT NULL,
	blockheight_leeway           BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES accounts(email)
);

CREATE TABLE contracts (
	id                      INT NOT NULL AUTO_INCREMENT,
	contract_id             VARCHAR(64) NOT NULL UNIQUE,
	start_height            BIGINT UNSIGNED NOT NULL,
	secret_key              VARCHAR(128) NOT NULL,
	download_spending       VARCHAR(64) NOT NULL,
	fund_account_spending   VARCHAR(64) NOT NULL,
	storage_spending        VARCHAR(64) NOT NULL,
	upload_spending         VARCHAR(64) NOT NULL,
	total_cost              VARCHAR(64) NOT NULL,
	contract_fee            VARCHAR(64) NOT NULL,
	txn_fee                 VARCHAR(64) NOT NULL,
	siafund_fee             VARCHAR(64) NOT NULL,
	account_balance_cost    VARCHAR(64) NOT NULL,
	fund_account_cost       VARCHAR(64) NOT NULL,
	update_price_table_cost VARCHAR(64) NOT NULL,
	good_for_upload         BOOL NOT NULL,
	good_for_renew          BOOL NOT NULL,
	bad_contract            BOOL NOT NULL,
	last_oos_err            BIGINT UNSIGNED NOT NULL,
	locked                  BOOL NOT NULL,
	renewed_from            VARCHAR(64) NOT NULL,
	renewed_to              VARCHAR(64) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE transactions (
	id                           INT NOT NULL AUTO_INCREMENT,
	contract_id                  VARCHAR(64) NOT NULL UNIQUE,
	parent_id                    VARCHAR(64) NOT NULL,
	uc_timelock                  BIGINT UNSIGNED NOT NULL,
	uc_renter_pk                 VARCHAR(128) NOT NULL,
	uc_host_pk                   VARCHAR(128) NOT NULL,
	signatures_required          INT NOT NULL,
	new_revision_number          BIGINT UNSIGNED NOT NULL,
	new_file_size                BIGINT UNSIGNED NOT NULL,
	new_file_merkle_root         VARCHAR(64) NOT NULL,
	new_window_start             BIGINT UNSIGNED NOT NULL,
	new_window_end               BIGINT UNSIGNED NOT NULL,
	new_valid_proof_output_0     VARCHAR(64) NOT NULL,
	new_valid_proof_output_uh_0  VARCHAR(64) NOT NULL,
	new_valid_proof_output_1     VARCHAR(64) NOT NULL,
	new_valid_proof_output_uh_1  VARCHAR(64) NOT NULL,
	new_missed_proof_output_0    VARCHAR(64) NOT NULL,
	new_missed_proof_output_uh_0 VARCHAR(64) NOT NULL,
	new_missed_proof_output_1    VARCHAR(64) NOT NULL,
	new_missed_proof_output_uh_1 VARCHAR(64) NOT NULL,
	new_missed_proof_output_2    VARCHAR(64) NOT NULL,
	new_missed_proof_output_uh_2 VARCHAR(64) NOT NULL,
	new_unlock_hash              VARCHAR(64) NOT NULL,
	t_parent_id_0                VARCHAR(64) NOT NULL,
	pk_index_0                   BIGINT UNSIGNED NOT NULL,
	timelock_0                   BIGINT UNSIGNED NOT NULL,
	signature_0                  VARCHAR(128) NOT NULL,
	t_parent_id_1                VARCHAR(64) NOT NULL,
	pk_index_1                   BIGINT UNSIGNED NOT NULL,
	timelock_1                   BIGINT UNSIGNED NOT NULL,
	signature_1                  VARCHAR(128) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (contract_id) REFERENCES contracts(contract_id)
);
