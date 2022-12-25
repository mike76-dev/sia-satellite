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
