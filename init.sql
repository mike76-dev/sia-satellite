/* wallet */

DROP TABLE IF EXISTS wt_sces;
DROP TABLE IF EXISTS wt_sfes;
DROP TABLE IF EXISTS wt_watched;
DROP TABLE IF EXISTS wt_addresses;
DROP TABLE IF EXISTS wt_tip;
DROP TABLE IF EXISTS wt_info;
DROP TABLE IF EXISTS wt_spent;

CREATE TABLE wt_addresses (
	id   BIGINT NOT NULL AUTO_INCREMENT,
	addr BINARY(32) NOT NULL UNIQUE,
	PRIMARY KEY (id)
);

CREATE TABLE wt_sces (
	id              BIGINT NOT NULL AUTO_INCREMENT,
	scoid           BINARY(32) NOT NULL UNIQUE,
	sc_value        BLOB NOT NULL,
	merkle_proof    BLOB NOT NULL,
	leaf_index      BIGINT UNSIGNED NOT NULL,
	maturity_height BIGINT UNSIGNED NOT NULL,
	address_id      BIGINT NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
);

CREATE TABLE wt_sfes (
	id              BIGINT NOT NULL AUTO_INCREMENT,
	sfoid           BINARY(32) NOT NULL UNIQUE,
	claim_start     BLOB NOT NULL,
	merkle_proof    BLOB NOT NULL,
	leaf_index      BIGINT UNSIGNED NOT NULL,
	sf_value        BIGINT UNSIGNED NOT NULL,
	address_id      BIGINT NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
);

CREATE TABLE wt_watched (
	address_id BIGINT NOT NULL UNIQUE,
	FOREIGN KEY (address_id) REFERENCES wt_addresses(id)
);

CREATE TABLE wt_tip (
	id        INT NOT NULL AUTO_INCREMENT,
	height    BIGINT UNSIGNED NOT NULL,
	bid       BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE wt_info (
	id       INT NOT NULL AUTO_INCREMENT,
	seed     BINARY(16) NOT NULL,
	progress BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE wt_spent (
	id BINARY(32) NOT NULL,
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
DROP TABLE IF EXISTS pt_accounts;
DROP TABLE IF EXISTS pt_stats;
DROP TABLE IF EXISTS pt_credits;
DROP TABLE IF EXISTS pt_announcement;

CREATE TABLE pt_accounts (
	id            INT NOT NULL AUTO_INCREMENT,
	email         VARCHAR(64) NOT NULL UNIQUE,
	password_hash BINARY(32) NOT NULL,
	verified      BOOL NOT NULL,
	time          BIGINT UNSIGNED NOT NULL,
	nonce         BINARY(16) NOT NULL,
	sc_address    BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

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

/* manager */

DROP TABLE IF EXISTS mg_email;
DROP TABLE IF EXISTS mg_timestamp;
DROP TABLE IF EXISTS mg_averages;
DROP TABLE IF EXISTS mg_spendings;
DROP TABLE IF EXISTS mg_balances;
DROP TABLE IF EXISTS mg_prices;
DROP TABLE IF EXISTS mg_maintenance;

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

CREATE TABLE mg_balances (
	email      VARCHAR(64) NOT NULL,
	subscribed BOOL NOT NULL,
	sc_balance DOUBLE NOT NULL,
	sc_locked  DOUBLE NOT NULL,
	currency   VARCHAR(8) NOT NULL,
	stripe_id  VARCHAR(32) NOT NULL,
	invoice    VARCHAR(32) NOT NULL,
	on_hold    BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (email),
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

/* hostdb */

DROP TABLE IF EXISTS hdb_scanhistory;
DROP TABLE IF EXISTS hdb_ipnets;
DROP TABLE IF EXISTS hdb_hosts;
DROP TABLE IF EXISTS hdb_fdomains;
DROP TABLE IF EXISTS hdb_fhosts;
DROP TABLE IF EXISTS hdb_contracts;
DROP TABLE IF EXISTS hdb_info;

CREATE TABLE hdb_hosts (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key BINARY(32) NOT NULL UNIQUE,
	filtered   BOOL NOT NULL,
	bytes      BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE hdb_scanhistory (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key BINARY(32) NOT NULL,
	time       BIGINT UNSIGNED NOT NULL,
	success    BOOL NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (public_key) REFERENCES hdb_hosts(public_key)
);

CREATE TABLE hdb_ipnets (
	id         INT NOT NULL AUTO_INCREMENT,
	public_key BINARY(32) NOT NULL,
	ip_net     VARCHAR(255) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (public_key) REFERENCES hdb_hosts(public_key)
);

CREATE TABLE hdb_fdomains (
	dom VARCHAR(255) NOT NULL
);

CREATE TABLE hdb_fhosts (
	public_key BINARY(32) NOT NULL
);

CREATE TABLE hdb_contracts (
	host_pk   BINARY(32) NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	data      BIGINT UNSIGNED NOT NULL
);

CREATE TABLE hdb_info (
	id               INT NOT NULL AUTO_INCREMENT,
	height           BIGINT UNSIGNED NOT NULL,
	bid              BINARY(32) NO NULL,
	scan_complete    BOOL NOT NULL,
	disable_ip_check BOOL NOT NULL,
	filter_mode      INT NOT NULL,
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
