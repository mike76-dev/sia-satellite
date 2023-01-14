DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS accounts;

CREATE TABLE accounts (
	id             INT NOT NULL AUTO_INCREMENT,
	email          VARCHAR(48) NOT NULL UNIQUE,
	password_hash  VARCHAR(64) NOT NULL,
	verified       BOOL NOT NULL,
	created        INT NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE balances (
	id         INT NOT NULL AUTO_INCREMENT,
	email      VARCHAR(48) NOT NULL,
	subscribed BOOL NOT NULL,
	balance    FLOAT NOT NULL,
	currency   VARCHAR(8) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES accounts(email)
);

CREATE TABLE payments (
	id         INT NOT NULL AUTO_INCREMENT,
	email      VARCHAR(48) NOT NULL,
	amount     FLOAT NOT NULL,
	currency   VARCHAR(8) NOT NULL,
	amount_usd FLOAT NOT NULL,
	made       INT NOT NULL,
	pending    BOOL NOT NULL,
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
