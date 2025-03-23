# Design of the Web Portal

## General

For the general concept, see:
- https://github.com/mike76-dev/sia-satellite
- https://ss-alpha.online
- [CONCEPT.md](https://github.com/mike76-dev/sia-satellite/blob/refurbish/CONCEPT.md)

The new site should look good on any screen, also on mobile devices.

There either needs to be a light/dark theme combination or a neutral theme (the latter is preferable).

The site should warn the visitor that it uses cookies for authentication only.

## Landing Page

The landing page should explain what Sia Satellite is and how it can benefit the users. It should contain a link to the personal dashboard.

The landing page should also contain the following (mostly in the footer):
- A link to GitHub repo (above)
- A link to the About page
- A link to the Privacy page
- A link to the TOC (Terms of Service) page
- A link to the Fees page
- A "Built with Sia" logo, linking to https://sia.tech

## Dashboard

### User Authentication

If a valid cookie is found, the user should be redirected to the Dashboard (a message may be displayed shortly).

If there is no cookie, the cookie is invalid or has expired, the Login window should be displayed. This window should offer the option to enter an E-mail address and a password or to click on a button to authenticate with Google. It should also offer an option to reset the password if it is forgotten or to sign up if the user has no account yet.

When the user chooses the signup option, they need to enter their E-mail address, choose a valid password and re-enter that password. Alternatively, they can use the Google authentication. They also need to agree with the Terms od Service by checking the box.

When the user has submitted their E-mail address and a password, a message should be displayed to check the mailbox and click on the link in the E-mail. This message should offer an option to resend the E-mail.

If the user has requested to reset the password, a similar message should be displayed with the option to resend the E-mail.

In case of any error returned by the server, an error message should be displayed.

### Front Page

The front page should contain the following widgets:
- Account information (E-mail address, verification status, payment plan, preferred currency) -> links to [Account](#account)
- Balance information (total, locked, available; both in Siacoin and in the preferred currency) -> links to [Payment](#payment)
- Contracts information (total, useful) -> links to [Contracts](#contracts)
- Objects information (count, total size, average health) -> links to [Objects](#objects)
- Network stats (block height, number of online hosts) -> links to [Hosts](#hosts)
- Usage stats (contracts formed/renewed/expired, slabs saved/migrated/retrieved/deleted in the current month) -> links to [Statistics](#statistics)

### Contracts

A paginated list of active contracts showing:
- Contract ID (with the option to copy)
- Host's net address and public key (with the option to copy)
- Formation and expiration dates (both dates and block heights)
- State (good/up to renewal/out of funds/bad)

It should be possible to delete the contracts individually, per selection, or altogether.

The page should also display the number of contracts (total/useful) and the current block height.

### Hosts

A paginated list of online hosts sorted by score showing:
- Host's net address (with the option to copy)
- Host's public key (with the option to copy)
- Price settings (contract/storage/ingress/egress)
- Total/remaining storage
- Country of location
- Black/whitelisting status

It should be possible to search a host by the net address or add to/remove from the blacklist or the whitelist.

The page should also display the total number of online hosts.

Following filters should be possible to apply:
- Maximal prices (contract, storage, ingress, egress)
- Maximal latency (in milliseconds)
- Minimal upload/download speeds (in megabytes/second)
- Basis for benchmarking (Europe, East-US, Asia, Global)
- Country of location

There should be an option to save this filter configuration.

### Objects

A paginated list of objects showing:
- Object name
- Object size
- Last modified timestamp
- Number of slabs
- Health

It should be possible to navigate through the objects like through a directory tree. It should also be possible to delete the objects individually, per selection, or altogether.

The page should also display the total number of objects, total file size, and the average health.

It should be possible to enter an encryption key to decrypt the metadata.

### Statistics

An overview of the usage in the current month compared to the previous month:
- Number of contracts formed
- Number of contracts renewed
- Number of contracts refreshed
- Number of contracts expired
- Number of slabs saved
- Number of slabs retrieved
- Number of slabs migrated
- Number of slabs deleted
- Satellite fees associated with each line, where applicable
- Total fees

It should be possible to navigate through the usage history month-wise.

### Payment

1. Account information
- Payment plan (pre-payment/invoicing)
- Remaining balance (both in Siacoin and in the preferred currency)
2. Amount calculation
- Projected upload/download input
- Payment currency selector
- Recommended amount
3. Making a payment
- Payment amount input (automatically populated from the previous step)
- If fiat payment: redirection to the Stripe payment
- If SC payment: display of the deposit address (with the copy option)
4. Paginated payment history sorted from most recent to least recent
- Timestamp
- Amount in payment currency
- Payment currency
- Amount in Siacoin
- Number of confirmations required (if paid in SC)
- Transaction ID (if paid in SC; with the copy option)

### Account

1. Account information
- Payment plan (pre-payment/invoicing)
- Option to switch the payment plan
2. Generate API token
- Option to select the token validity
- Option to copy the generated token
3. Enabling/disabling the opt-in settings
- Manage contracts
- Backup file metadata
- Auto-repair files
4. Change password (the user should enter and re-enter the new password)
5. Delete the account (user confirmation required)

### Navigation

- Link to the landing page
- Link to the front page
- Contracts
- Hosts
- Objects (Files)
- Statistics
- Payment
- Account

### Footer

- About
- Privacy
- Terms of Service
- Fees
- Help
- "Built with Sia" logo (linking to https://sia.tech)
- Satellite version