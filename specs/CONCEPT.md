# Sia Satellite Concept

## Design

The Satellite concept consists of three major parts:
1. The Satellite server (further Satellite), which performs the contract and data management.
2. The Satellite client (further Client), which interacts both with the Satellite and with `renterd` via the API.
3. The web portal, including the personal dashboard.

The difference with the previous design is the absence of the `renterd` fork. This one is replaced with the Client. It is still required to run a piece of software locally because of trust issues. But now the software is much easier to maintain.

## Using the Satellite

### Managing the Contracts

1. The user registers an account with the Satellite and deposits some funds.
2. The user enables managing contracts in the Satellite dashboard.
3. The user requests an API key in the Satellite dashboard.
4. The user runs the Client alongside `renterd`.
5. The Client retrieves the `renterd` settings and instructs the Satellite to form the required number of contracts.
6. The contracts are imported in `renterd`.
7. The Satellite monitors the contracts and renews/refreshes them, when needed.
8. The user can view the contracts in the dashboard and delete any of them.
9. The Client periodically checks if there are any new contracts on the Satellite and imports them in `renterd`.

### Managing the Files

1. The user opts in for backing up the file metadata on the Satellite.
2. The user configures the polling frequency in the Client.
3. The Client checks periodically if the object metadata has changed in `renterd`. If that is the case, the changed metadata is transferred to the Satellite.
4. If a file gets lost in `renterd`, the Client imports the metadata from the Satellite.
5. The user can view and/or delete the stored metadata in the dashboard.
6. In order to keep privacy of the data, the user can encrypt the metadata with an encryption key configurable in the Client. To view the metadata in the dashboard, the user can use the encryption key to decrypt it.

### File Repairs

1. The user opts in for backing up the file metadata and for automatic file repairs.
2. The Satellite monitors the health of the data. If it falls below a certain threshold, the data is migrated to the other hosts.
3. The Client checks if the timestamp of any stored slab is newer than of the existing one. In that case, the new slab is imported in `renterd`.

### Direct Uploads

The user can use an API endpoint to upload a file directly to the Satellite to save on the bandwidth. The Satellite erasure-codes the data and uploads it to the network. To preserve the privacy, the data can be encrypted with the same key that is used for encrypting file metadata.

### Using the Satellite Directly

The user can use the API to connect to the Satellite directly, without running the Client.