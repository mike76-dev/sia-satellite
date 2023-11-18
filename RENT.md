# How to Rent Storage Using a Satellite

In the future, Satellite support will be built into [`renterd`](https://github.com/SiaFoundation/renterd). Currently, a slightly modified version of `renterd` is available at [https://github.com/mike76-dev/renterd](https://github.com/mike76-dev/renterd).

This version is fully compatible with the original `renterd` metadata-wise. This means that you can run it over your existing installation, and when you're done with playing around, you can install the original version back.

It's also very easy to switch the Satellite mode on and off, and also change the Satellite. With the Satellite enabled, your `renterd` will be forming contracts using the Satellite you chose. When you disable the Satellite, it will be using its own wallet.

## Download the Renting Software

Download and install the latest version from [https://github.com/mike76-dev/renterd/releases](https://github.com/mike76-dev/renterd/releases). If you prefer to build from source, make sure you're building the `satellite` branch and not `master`.

You don't need any Siacoins to rent storage using a Satellite, but your `renterd` still needs a wallet seed to form the storage contracts. If you haven't done so yet, generate a new wallet seed with the following command:
```
renterd seed
```
Take a note of these 12 words. If you don't want to enter the seed each time `renterd` starts, you can put it in the `RENTERD_SEED` environment variable (the exact way to do it depends on your OS).

You also need to chose the API password, which will be used by the UI. You can either enter it on each startup or put it in the `RENTERD_API_PASSWORD` environment variable.

Now start `renterd`. Replace `<PATH>` with the actual folder name where the databases will be stored:
```
renterd --dir="<PATH>"
```
If this is a fresh installation, `renterd` will now be syncing to the blockchain. You can go to the following step in the meantime.

## Register With a Satellite

You need to register an account with the Satellite you choose. When the Satellite verifies your email address, you will be redirected to your dashboard. From there, you can see the network stats, the contracts that the Satellite has formed on your behalf, the payments you have made, and also perform the payments and manage your account.

Once a payment has been made to your account, you will be able to see the Satellite details on the Account page. Take a note of the Satellite public key and the renter seed.

## Configure the Renting Software

Once your `renterd` is fully synced, navigate to the Satellite page and enter the following configuration:
```
Address:     the network address of the Satellite including the port number
Mux Port:    the port of the Satellite listening to RHP3 calls (defaults to 9993)
Public Key:  the public key displayed on the Account page of the Satellite portal,
             without the 'ed25519:' prefix
Renter Seed: the renter seed displayed on the Account page of the Satellite portal
```
When you have finished, enable the Satellite and save the config.

Now you're all set!

## Opt-In Settings

There is a number of settings on the Satellite page, which enable or disable some additional features. Most of them imply sharing a derived (non-master) private key with the Satellite. When you disable an option, any sensitive information is removed from the Satellite.

### Auto Renew Contracts
When you enable this, the Satellite will try to renew any contract that is about to expire. The autopilot of `renterd` will import any renewed contracts on startup as well as during each contract maintenance. This allows you to run `renterd` less frequently, but you need to keep in mind that only `renterd` can still form new contracts, so if the renewals eventually fail, your contract set and the data stored will slowly degrade.

### Backup File Metadata
By enabling this option, `renterd` will save any file metadata on the Satellite when you upload a file. Then, if you delete a file accidentally in `renterd`, or if your data becomes corrupt, `renterd` will import the file back from the satellite. If you want to delete a file permanently, you need to do it also in the Satellite dashboard.

### File Auto Repair
This option needs both _Auto Renew Contracts_ and _Backup File Metadata_ to be enabled to work. When you enable it, the Satellite takes over almost all contract maintenance: contract formations, renewals, and refreshes, as well as monitoring the file health and migrating unhealthy slabs to new hosts. Enabling this option allows you to turn on `renterd` only if you need to upload or download files, which you can do as often as you like, even once in a few years. The only thing you need to care of is maintaining a positive balance with the Satellite.

### Proxy Uploads through Satellite
When you enable this option, you upload your files directly to the Satellite, which saves your bandwidth and is usually much faster. The Satellite then uploads these files to the network in the background. The downside of this is that you don't see the uploaded files in `renterd` immediately, but only on the next autopilot loop iteration, when the file metadata is imported from the Satellite.

IMPORTANT: Please note that S3 interface is not fully supported yet.

## Data Encryption

The `-satellite` versions of `renterd` now encrypt the files before uploading. If you choose to backup the file metadata on the Satellite, the Satellite operator will not be able to read the contents of the files or even their names. This way, you can enjoy the full privacy when sharing your file metadata with the Satellite.

The Satellite page of the `renterd` UI shows the encryption key used to encrypt your data. You can copy this key end paste it on your dashboard of the Satellite web portal, this will allow you to view your saved files and download them from the dashboard. The key is not transferred to the Satellite, which means that only you can access your data.