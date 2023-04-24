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

Now start `renterd`. It's recommended to increase the upload timeout, because the default 5 seconds have proven to be too short:
```
renterd --worker.uploadSectorTimeout=60s
```
If this is a fresh installation, `renterd` will now be syncing to the blockchain. You can go to the following step in the meantime.

## Register With a Satellite

You need to register an account with the Satellite you choose. When the Satellite verifies your email address, you will be redirected to your dashboard. From there, you can see the network stats, the contracts that the Satellite has formed on your behalf, the payments you have made, and also perform the payments and manage your account.

Once a payment has been made to your account, you will be able to see the Satellite details on the Account page. Take a note of the Satellite public key and the renter seed.

## Configure the Renting Software

Once your `renterd` is fully synced, navigate to the Satellite page and enter the following configuration:
```
Address:     the network address of the Satellite including the port number
Public Key:  the public key displayed on the Account page of the Satellite portal,
             without the 'ed25519:' prefix
Renter Seed: the renter seed displayed on the Account page of the Satellite portal
```
When you have finished, enable the Satellite and save the config.

Now you're all set!
