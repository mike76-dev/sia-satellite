/* gateway */

DROP TABLE IF EXISTS gw_nodes;
DROP TABLE IF EXISTS gw_url;
DROP TABLE IF EXISTS gw_blocklist;

CREATE TABLE gw_nodes (
	address  VARCHAR(255) NOT NULL,
	outbound BOOL,
	PRIMARY KEY (address)
);

CREATE TABLE gw_url (
	router_url VARCHAR(255) NOT NULL
);

INSERT INTO gw_url (router_url) VALUES ('');

CREATE TABLE gw_blocklist (
	ip VARCHAR(255) NOT NULL,
	PRIMARY KEY (ip)
);

/* consensus */

DROP TABLE IF EXISTS cs_height;
DROP TABLE IF EXISTS cs_consistency;
DROP TABLE IF EXISTS cs_sfpool;
DROP TABLE IF EXISTS cs_changelog;
DROP TABLE IF EXISTS cs_dsco;
DROP TABLE IF EXISTS cs_fcex;
DROP TABLE IF EXISTS cs_oak;
DROP TABLE IF EXISTS cs_oak_init;
DROP TABLE IF EXISTS cs_sco;
DROP TABLE IF EXISTS cs_fc;
DROP TABLE IF EXISTS cs_sfo;
DROP TABLE IF EXISTS cs_fuh;
DROP TABLE IF EXISTS cs_fuh_current;
DROP TABLE IF EXISTS cs_map;
DROP TABLE IF EXISTS cs_path;
DROP TABLE IF EXISTS cs_cl;
DROP TABLE IF EXISTS cs_dos;

CREATE TABLE cs_height (
	id     INT NOT NULL AUTO_INCREMENT,
	height BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_consistency (
	id            INT NOT NULL AUTO_INCREMENT,
	inconsistency BOOL NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_sfpool (
	id    INT NOT NULL AUTO_INCREMENT,
	bytes VARBINARY(24) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_changelog (
	id    INT NOT NULL AUTO_INCREMENT,
	bytes BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_dsco (
	height BIGINT UNSIGNED NOT NULL,
	scoid  BINARY(32) NOT NULL,
	bytes  VARBINARY(56) NOT NULL,
	PRIMARY KEY (scoid ASC)
);

CREATE TABLE cs_fcex (
	height BIGINT UNSIGNED NOT NULL,
	fcid   BINARY(32) NOT NULL,
	bytes  BLOB NOT NULL,
	PRIMARY KEY (fcid ASC)
);

CREATE TABLE cs_oak (
	bid   BINARY(32) NOT NULL UNIQUE,
	bytes BINARY(40) NOT NULL,
	PRIMARY KEY (bid ASC)
);

CREATE TABLE cs_oak_init (
	id   INT NOT NULL AUTO_INCREMENT,
	init BOOL NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_sco (
	scoid BINARY(32) NOT NULL,
	bytes VARBINARY(56) NOT NULL,
	PRIMARY KEY (scoid ASC)
);

CREATE TABLE cs_fc (
	fcid  BINARY(32) NOT NULL,
	bytes BLOB NOT NULL,
	PRIMARY KEY (fcid ASC)
);

CREATE TABLE cs_sfo (
	sfoid BINARY(32) NOT NULL,
	bytes VARBINARY(80) NOT NULL,
	PRIMARY KEY (sfoid ASC)
);

CREATE TABLE cs_fuh (
	height BIGINT UNSIGNED NOT NULL,
	bytes  BINARY(64) NOT NULL,
	PRIMARY KEY (height ASC)
);

CREATE TABLE cs_fuh_current (
	id     INT NOT NULL AUTO_INCREMENT,
	bytes  BINARY(64) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_path (
	height BIGINT UNSIGNED NOT NULL,
	bid    BINARY(32) NOT NULL,
	PRIMARY KEY (height ASC)
);

CREATE TABLE cs_map (
	id    INT NOT NULL AUTO_INCREMENT,
	bid   BINARY(32) NOT NULL UNIQUE,
	bytes LONGBLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE cs_cl (
	ceid  BINARY(32) NOT NULL,
	bytes VARBINARY(1024) NOT NULL,
	PRIMARY KEY (ceid ASC)
);

CREATE TABLE cs_dos (
	bid BINARY(32) NOT NULL,
	PRIMARY KEY (bid ASC)
);

/* transactionpool */

DROP TABLE IF EXISTS tp_height;
DROP TABLE IF EXISTS tp_ctx;
DROP TABLE IF EXISTS tp_median;
DROP TABLE IF EXISTS tp_cc;
DROP TABLE IF EXISTS tp_recent;

CREATE TABLE tp_height (
	id     INT NOT NULL AUTO_INCREMENT,
	height BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE tp_ctx (
	txid BINARY(32) NOT NULL,
	PRIMARY KEY (txid),
	INDEX txid (txid ASC)
);

CREATE TABLE tp_median (
	id    INT NOT NULL AUTO_INCREMENT,
	bytes BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE tp_cc (
	id   INT NOT NULL AUTO_INCREMENT,
	ceid BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE tp_recent (
	id  INT NOT NULL AUTO_INCREMENT,
	bid BINARY(32) NOT NULL,
	PRIMARY KEY (id)
);

/* wallet */

DROP TABLE IF EXISTS wt_addr;
DROP TABLE IF EXISTS wt_txn;
DROP TABLE IF EXISTS wt_sco;
DROP TABLE IF EXISTS wt_sfo;
DROP TABLE IF EXISTS wt_spo;
DROP TABLE IF EXISTS wt_uc;
DROP TABLE IF EXISTS wt_info;
DROP TABLE IF EXISTS wt_watch;
DROP TABLE IF EXISTS wt_aux;
DROP TABLE IF EXISTS wt_keys;

CREATE TABLE wt_txn (
	id    INT NOT NULL AUTO_INCREMENT,
	txid  BINARY(32) NOT NULL UNIQUE,
	bytes BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE wt_addr (
	id   INT NOT NULL AUTO_INCREMENT,
	addr BINARY(32) NOT NULL,
	txid BINARY(32) NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (txid) REFERENCES wt_txn(txid)
);

CREATE TABLE wt_sco (
	scoid BINARY(32) NOT NULL,
	bytes VARBINARY(56) NOT NULL,
	PRIMARY KEY (scoid ASC)
);

CREATE TABLE wt_sfo (
	sfoid BINARY(32) NOT NULL,
	bytes VARBINARY(80) NOT NULL,
	PRIMARY KEY (sfoid ASC)
);

CREATE TABLE wt_spo (
	oid    BINARY(32) NOT NULL,
	height BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (oid ASC)
);

CREATE TABLE wt_uc (
	addr  BINARY(32) NOT NULL,
	bytes BLOB NOT NULL,
	PRIMARY KEY (addr ASC)
);

CREATE TABLE wt_info (
	id        INT NOT NULL AUTO_INCREMENT,
	cc        BINARY(32) NOT NULL,
	height    BIGINT UNSIGNED NOT NULL,
	encrypted BLOB NOT NULL,
	sfpool    VARBINARY(24) NOT NULL,
	salt      BINARY(32) NOT NULL,
	progress  BIGINT UNSIGNED NOT NULL,
	seed      BLOB NOT NULL,
	pwd       BLOB NOT NULL,
	PRIMARY KEY (id)
);

CREATE TABLE wt_aux (
	salt      BINARY(32) NOT NULL,
	encrypted BLOB NOT NULL,
	seed      BLOB NOT NULL,
	PRIMARY KEY (seed(32))
);

CREATE TABLE wt_keys (
	salt      BINARY(32) NOT NULL,
	encrypted BLOB NOT NULL,
	skey      BLOB NOT NULL,
	PRIMARY KEY (skey(32))
);

CREATE TABLE wt_watch (
	addr BINARY(32) NOT NULL,
	PRIMARY KEY (addr ASC)
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

/* manager */

DROP TABLE IF EXISTS mg_email;
DROP TABLE IF EXISTS mg_timestamp;
DROP TABLE IF EXISTS mg_averages;
DROP TABLE IF EXISTS mg_spendings;
DROP TABLE IF EXISTS mg_balances;
DROP TABLE IF EXISTS mg_prices;

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
	email                   VARCHAR(64) NOT NULL,
	current_locked          DOUBLE NOT NULL,
	current_used            DOUBLE NOT NULL,
	current_overhead        DOUBLE NOT NULL,
	prev_locked             DOUBLE NOT NULL,
	prev_used               DOUBLE NOT NULL,
	prev_overhead           DOUBLE NOT NULL,
	current_formed          BIGINT UNSIGNED NOT NULL,
	current_renewed         BIGINT UNSIGNED NOT NULL,
	current_slabs_saved     BIGINT UNSIGNED NOT NULL,
	current_slabs_retrieved BIGINT UNSIGNED NOT NULL,
	current_slabs_migrated  BIGINT UNSIGNED NOT NULL,
	prev_formed             BIGINT UNSIGNED NOT NULL,
	prev_renewed            BIGINT UNSIGNED NOT NULL,
	prev_slabs_saved        BIGINT UNSIGNED NOT NULL,
	prev_slabs_retrieved    BIGINT UNSIGNED NOT NULL,
	prev_slabs_migrated     BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (email),
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
	retrieve_metadata_prepayment DOUBLE NOT NULL,
	retrieve_metadata_invoicing  DOUBLE NOT NULL,
	migrate_slab_prepayment      DOUBLE NOT NULL,
	migrate_slab_invoicing       DOUBLE NOT NULL,
	PRIMARY KEY (id)
)

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
	scan_complete    BOOL NOT NULL,
	disable_ip_check BOOL NOT NULL,
	last_change      BINARY(32) NOT NULL,
	filter_mode      INT NOT NULL,
	PRIMARY KEY (id)
);

/* contractor */

DROP TABLE IF EXISTS ctr_contracts;
DROP TABLE IF EXISTS ctr_renters;
DROP TABLE IF EXISTS ctr_info;
DROP TABLE IF EXISTS ctr_dspent;
DROP TABLE IF EXISTS ctr_watchdog;
DROP TABLE IF EXISTS ctr_shards;
DROP TABLE IF EXISTS ctr_slabs;
DROP TABLE IF EXISTS ctr_metadata;

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
	PRIMARY KEY (id),
	FOREIGN KEY (email) REFERENCES pt_accounts(email)
);

CREATE TABLE ctr_contracts (
	id           BINARY(32) NOT NULL,
	renter_pk    BINARY(32) NOT NULL,
	renewed_from BINARY(32) NOT NULL,
	renewed_to   BINARY(32) NOT NULL,
	imported     BOOL NOT NULL,
	bytes        BLOB NOT NULL,
	PRIMARY KEY (id),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_info (
	id          INT NOT NULL AUTO_INCREMENT,
	height      BIGINT UNSIGNED NOT NULL,
	last_change BINARY(32) NOT NULL,
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
	enc_key   BINARY(32) NOT NULL,
	filepath  VARCHAR(255) NOT NULL,
	renter_pk BINARY(32) NOT NULL,
	uploaded  BIGINT UNSIGNED NOT NULL,
	modified  BIGINT UNSIGNED NOT NULL,
	retrieved BIGINT UNSIGNED NOT NULL,
	PRIMARY KEY (enc_key),
	FOREIGN KEY (renter_pk) REFERENCES ctr_renters(public_key)
);

CREATE TABLE ctr_slabs (
	enc_key    BINARY(32) NOT NULL,
	object_id  BINARY(32) NOT NULL,
	min_shards INT UNSIGNED NOT NULL,
	offset     BIGINT UNSIGNED NOT NULL,
	len        BIGINT UNSIGNED NOT NULL,
	num        INT NOT NULL,
	PRIMARY KEY (enc_key),
	FOREIGN KEY (object_id) REFERENCES ctr_metadata(enc_key)
);

CREATE TABLE ctr_shards (
	slab_id     BINARY(32) NOT NULL,
	host        BINARY(32) NOT NULL,
	merkle_root BINARY(32) NOT NULL,
	FOREIGN KEY (slab_id) REFERENCES ctr_slabs(enc_key)
);
