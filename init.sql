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
