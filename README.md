# Sia Satellite
Sia is a decentralized cloud storage platform where the renters form storage contracts with the hosts in an open marketspace and pay them in Siacoins (SC). The open marketspace ensures that the renters pay a competitive price for the data they store but can also expect a reasonably good quality of the service provided by the hosts. The decentralized nature of Sia ensures that there is no single entity controlling the renter’s data which, combined with a sufficient redundancy, means that the data will remain available as long as it is paid for, even if a part of the hosts goes offline or disappears entirely.

Unfortunately, you can currently only use the Sia storage platform if you have Siacoins. For most of the users, those who have just heard about crypto currencies but never actually used them, this means an additional barrier. To purchase SC, one first needs to open an account with a crypto exchange, often go through a KYC process, deposit fiat money, buy Bitcoins (because on most exchanges SC can only be traded for BTC), and exchange them for Siacoins. All this significantly hinders a broader adoption of Sia.

**Sia Satellite** is a business model that can help overcome this barrier, at the cost of some centralization. A Satellite is a network service that forms contracts with the hosts on behalf of the renter, manages these contracts, keeps track of the spendings, and pays the hosts with SC. The renter uploads their data directly to the hosts and downloads from them, thus reducing the load on the Satellite. The renter pays for the storage in fiat money using their credit card.

When using **Sia Satellite**, a renter does not need to own SC to use Sia storage, nor do they need to know about SC at all.
**Sia Satellite** consists of the following two parts:
- The Satellite service, run on a remote server by a third party (the service provider)
- The web portal, used to register user accounts, provide usage analytics, and pay for the storage

## Guides

A guide on how to set up a Satellite node can be found [here](SETUP.md).
A guide on how to rent storage using a Satellite can be found [here](RENT.md).

## Acknowledgement

This work is supported by a [Sia Foundation](https://sia.tech) grant.
