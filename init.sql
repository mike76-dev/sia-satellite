/* wallet */

DROP TABLE IF EXISTS wt_sces;
DROP TABLE IF EXISTS wt_addrs;
DROP TABLE IF EXISTS wt_tip;

CREATE TABLE wt_addrs (
	id   BIGINT UNSIGNED NOT NULL,
	addr BINARY(32) NOT NULL UNIQUE
);

CREATE TABLE wt_sces (
	scoid BINARY(32) NOT NULL UNIQUE,
	bytes BLOB NOT NULL
);

CREATE TABLE wt_tip (
	id     INT NOT NULL AUTO_INCREMENT,
	height BIGINT UNSIGNED NOT NULL,
	bid    BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

/* hostdb */

DROP TABLE IF EXISTS hdb_interactions;
DROP TABLE IF EXISTS hdb_hosts;

CREATE TABLE hdb_hosts (
	is_online          BOOL NOT NULL,
	public_key         BINARY(32) NOT NULL,
	first_seen         BIGINT NOT NULL,
	known_since        BIGINT UNSIGNED NOT NULL,
	net_address        TEXT NOT NULL,
	ip_nets            TEXT NOT NULL,
	last_ip_change     BIGINT NOT NULL,
	price_score        DOUBLE NOT NULL,
	storage_score      DOUBLE NOT NULL,
	collateral_score   DOUBLE NOT NULL,
	interactions_score DOUBLE NOT NULL,
	uptime_score       DOUBLE NOT NULL,
	age_score          DOUBLE NOT NULL,
	version_score      DOUBLE NOT NULL,
	latency_score      DOUBLE NOT NULL,
	benchmarks_score   DOUBLE NOT NULL,
	contracts_score    DOUBLE NOT NULL,
	total_score        DOUBLE NOT NULL,
	country            TEXT NOT NULL,
	settings           BLOB,
	PRIMARY KEY (public_key)
);

CREATE TABLE hdb_interactions (
	public_key         BINARY(32) NOT NULL,
	node               VARCHAR(8) NOT NULL,
	uptime             BIGINT NOT NULL,
	downtime           BIGINT NOT NULL,
	last_seen          BIGINT NOT NULL,
	active_hosts       INT NOT NULL,
	price_score        DOUBLE NOT NULL,
	storage_score      DOUBLE NOT NULL,
	collateral_score   DOUBLE NOT NULL,
	interactions_score DOUBLE NOT NULL,
	uptime_score       DOUBLE NOT NULL,
	age_score          DOUBLE NOT NULL,
	version_score      DOUBLE NOT NULL,
	latency_score      DOUBLE NOT NULL,
	benchmarks_score   DOUBLE NOT NULL,
	contracts_score    DOUBLE NOT NULL,
	total_score        DOUBLE NOT NULL,
	successes          DOUBLE NOT NULL,
	failures           DOUBLE NOT NULL,
	PRIMARY KEY (public_key, node),
	FOREIGN KEY (public_key) REFERENCES hdb_hosts(public_key)
);

/* account manager */

DROP TABLE IF EXISTS am_accounts;
DROP TABLE IF EXISTS am_info;

CREATE TABLE am_accounts (
	id            INT NOT NULL AUTO_INCREMENT,
	email         VARCHAR(64) NOT NULL UNIQUE,
	password_hash BINARY(32) NOT NULL,
	created_at    BIGINT NOT NULL,
	verified      BOOL NOT NULL,
	invoicing     BOOL NOT NULL,
	sc_total      BLOB NOT NULL,
	sc_locked     BLOB NOT NULL,
	currency      VARCHAR(8) NOT NULL,
	stripe_id     VARCHAR(32) NOT NULL,
	invoice       VARCHAR(32) NOT NULL,
	on_hold       BIGINT NOT NULL,
	nonce         BINARY(16) NOT NULL,
	sc_address    BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE am_info (
	id          INT NOT NULL AUTO_INCREMENT,
	private_key BINARY(64) NOT NULL,
	PRIMARY KEY (id)
);

/* provider */

DROP TABLE IF EXISTS pr_info;

CREATE TABLE pr_info (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key BINARY(32) NOT NULL,
	secret_key BINARY(64) NOT NULL,
	address    VARCHAR(64) NOT NULL,
	PRIMARY KEY (id)
);

/* portal */

DROP TABLE IF EXISTS pt_payments;
DROP TABLE IF EXISTS pt_stats;
DROP TABLE IF EXISTS pt_credits;
DROP TABLE IF EXISTS pt_announcement;
DROP TABLE IF EXISTS pt_tip;

CREATE TABLE pt_payments (
	id        INT NOT NULL AUTO_INCREMENT,
	email     VARCHAR(64) NOT NULL,
	amount    DOUBLE NOT NULL,
	currency  VARCHAR(8) NOT NULL,
	amount_sc DOUBLE NOT NULL,
	made_at   INT NOT NULL,
	conf_left INT NOT NULL,
	txid      BINARY(32) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES pt_accounts(email)
);

CREATE TABLE pt_stats (
	remote_host  VARCHAR(64) NOT NULL,
	login_last   BIGINT NOT NULL,
	login_count  BIGINT NOT NULL,
	verify_last  BIGINT NOT NULL,
	verify_count BIGINT NOT NULL,
	reset_last   BIGINT NOT NULL,
	reset_count  BIGINT NOT NULL,
	PRIMARY KEY (remote_host)
);

CREATE TABLE pt_credits (
	id        INT NOT NULL AUTO_INCREMENT,
	amount    DOUBLE NOT NULL,
	remaining BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE pt_announcement (
	id           INT NOT NULL AUTO_INCREMENT,
	announcement TEXT NOT NULL,
	expires      BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE pt_tip (
	id        INT NOT NULL AUTO_INCREMENT,
	height    BIGINT UNSIGNED NOT NULL,
	bid       BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

/* manager */

DROP TABLE IF EXISTS mg_email;
DROP TABLE IF EXISTS mg_timestamp;
DROP TABLE IF EXISTS mg_averages;
DROP TABLE IF EXISTS mg_spendings;
DROP TABLE IF EXISTS mg_prices;
DROP TABLE IF EXISTS mg_maintenance;
DROP TABLE IF EXISTS mg_tip;

CREATE TABLE mg_email (
	id        INT NOT NULL AUTO_INCREMENT,
	email     VARCHAR(64) NOT NULL,
	threshold VARBINARY(24) NOT NULL,
	time_sent BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE mg_timestamp (
	id     INT NOT NULL AUTO_INCREMENT,
	height BIGINT UNSIGNED NOT NULL,
	time   BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE mg_averages (
	id    INT NOT NULL AUTO_INCREMENT,
	bytes BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE mg_spendings (
	email           VARCHAR(64) NOT NULL,
	period          CHAR(6) NOT NULL,
	locked          DOUBLE NOT NULL,
	used            DOUBLE NOT NULL,
	overhead        DOUBLE NOT NULL,
	formed          BIGINT UNSIGNED NOT NULL,
	renewed         BIGINT UNSIGNED NOT NULL,
	slabs_saved     BIGINT UNSIGNED NOT NULL,
	slabs_retrieved BIGINT UNSIGNED NOT NULL,
	slabs_migrated  BIGINT UNSIGNED NOT NULL,
	CONSTRAINT email_period UNIQUE (email, period),
	FOREIGN KEY (email) REFERENCES pt_accounts(email)
);

CREATE TABLE mg_prices (
	id INT NOT NULL AUTO_INCREMENT,
	form_contract_prepayment     DOUBLE NOT NULL,
	form_contract_invoicing      DOUBLE NOT NULL,
	save_metadata_prepayment     DOUBLE NOT NULL,
	save_metadata_invoicing      DOUBLE NOT NULL,
	store_metadata_prepayment    DOUBLE NOT NULL,
	store_metadata_invoicing     DOUBLE NOT NULL,
	store_partial_prepayment     DOUBLE NOT NULL,
	store_partial_invoicing      DOUBLE NOT NULL,
	retrieve_metadata_prepayment DOUBLE NOT NULL,
	retrieve_metadata_invoicing  DOUBLE NOT NULL,
	migrate_slab_prepayment      DOUBLE NOT NULL,
	migrate_slab_invoicing       DOUBLE NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE mg_maintenance (
	id          INT NOT NULL AUTO_INCREMENT,
	maintenance BOOL NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE mg_tip (
	id        INT NOT NULL AUTO_INCREMENT,
	height    BIGINT UNSIGNED NOT NULL,
	bid       BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

/* contractor */

DROP TABLE IF EXISTS ctr_contracts;
DROP TABLE IF EXISTS ctr_uploads;
DROP TABLE IF EXISTS ctr_info;
DROP TABLE IF EXISTS ctr_dspent;
DROP TABLE IF EXISTS ctr_watchdog;
DROP TABLE IF EXISTS ctr_shards;
DROP TABLE IF EXISTS ctr_slabs;
DROP TABLE IF EXISTS ctr_metadata;
DROP TABLE IF EXISTS ctr_parts;
DROP TABLE IF EXISTS ctr_multipart;
DROP TABLE IF EXISTS ctr_renters;

CREATE TABLE ctr_renters (
	id                   INT NOT NULL AUTO_INCREMENT,
	email                VARCHAR(64) NOT NULL,
	public_key           BINARY(32) NOT NULL UNIQUE,
	current_period       BIGINT UNSIGNED NOT NULL,
	allowance            BLOB NOT NULL,
	private_key          BINARY(64),
	account_key          BINARY(64),
	auto_renew_contracts BOOL NOT NULL,
	backup_file_metadata BOOL NOT NULL,
	auto_repair_files    BOOL NOT NULL,
	proxy_uploads        BOOL NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES pt_accounts(email)
);

CREATE TABLE ctr_contracts (
	id           BINARY(32) NOT NULL,
	renter_pk    BINARY(32) NOT NULL,
	renewed_from BINARY(32) NOT NULL,
	renewed_to   BINARY(32) NOT NULL,
	unlocked     BOOL NOT NULL,
	imported     BOOL NOT NULL,
	bytes        BLOB NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_info (
	id          INT NOT NULL AUTO_INCREMENT,
	height      BIGINT UNSIGNED NOT NULL,
	bid         BINARY(32) NOT NULL,
	synced      BOOL NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE ctr_dspent (
	id     BINARY(32) NOT NULL,
	height BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE ctr_watchdog (
	id    BINARY(32) NOT NULL,
	bytes BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE ctr_metadata (
	id        BINARY(32) NOT NULL,
	enc_key   BINARY(32) NOT NULL,
	bucket    BLOB NOT NULL,
	filepath  BLOB NOT NULL,
	etag      VARCHAR(64) NOT NULL,
	mime      BLOB NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	uploaded  BIGINT UNSIGNED NOT NULL,
	modified  BIGINT UNSIGNED NOT NULL,
	retrieved BIGINT UNSIGNED NOT NULL,
	encrypted TEXT NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_slabs (
	enc_key    BINARY(32) NOT NULL,
	object_id  BINARY(32) NOT NULL,
	renter_pk  BINARY(32) NOT NULL,
	min_shards INT UNSIGNED NOT NULL,
	offset     BIGINT UNSIGNED NOT NULL,
	len        BIGINT UNSIGNED NOT NULL,
	num        INT NOT NULL,
	partial    BOOL NOT NULL,
	orphan     BOOL NOT NULL,
	modified   BIGINT UNSIGNED NOT NULL,
	retrieved  BIGINT UNSIGNED NOT NULL,
	data       LONGBLOB,
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_shards (
	slab_id     BINARY(32) NOT NULL,
	host        BINARY(32) NOT NULL,
	merkle_root BINARY(32) NOT NULL
);

CREATE TABLE ctr_uploads (
	filename  CHAR(20) NOT NULL,
	bucket    BLOB NOT NULL,
	filepath  BLOB NOT NULL,
	mime      BLOB NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	ready     BOOL NOT NULL,
	encrypted TEXT NOT NULL,
	PRIMARY KEY (filename),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_multipart (
	id        BINARY(32) NOT NULL,
	enc_key   BINARY(32) NOT NULL,
	bucket    BLOB NOT NULL,
	filepath  BLOB NOT NULL,
	mime      BLOB NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	created   BIGINT UNSIGNED NOT NULL,
	encrypted BOOL NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_parts (
	filename  CHAR(20) NOT NULL,
	num       INT NOT NULL,
	upload_id BINARY(32) NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	PRIMARY KEY (filename),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);
