# Sia Satellite
Sia is a decentralized cloud storage platform where the renters form storage contracts with the hosts in an open marketspace and pay them in Siacoins (SC). The open marketspace ensures that the renters pay a competitive price for the data they store but can also expect a reasonably good quality of the service provided by the hosts. The decentralized nature of Sia ensures that there is no single entity controlling the renter’s data which, combined with a sufficient redundancy, means that the data will remain available as long as it is paid for, even if a part of the hosts goes offline or disappears entirely.

Unfortunately, you can currently only use the Sia storage platform if you have Siacoins. For most of the users, those who have just heard about crypto currencies but never actually used them, this means an additional barrier. To purchase SC, one first needs to open an account with a crypto exchange, often go through a KYC process, deposit fiat money, buy Bitcoins (because on most exchanges SC can only be traded for BTC), and exchange them for Siacoins. All this significantly hinders a broader adoption of Sia.

**Sia Satellite** is a business model that can help overcome this barrier, at the cost of some centralization. A satellite is a network service that forms contracts with the hosts on behalf of the renter, manages these contracts, keeps track of the spendings, and pays the hosts with SC. The renter uploads their data directly to the hosts and downloads from them, thus reducing the load on the satellite. At the end of each period (usually one month), the renter pays for the service with their credit card. An upfront payment is also possible. An upfront-paying user can enjoy certain benefits, like setting price limits or selecting the hosts to store their data with.

When using **Sia Satellite**, a renter does not need to own SC to use Sia storage, nor do they need to know about SC at all.
**Sia Satellite** consists of the following two parts:
- The satellite service, run on a remote server by a third party (the service provider)
- The web portal, used to register user accounts, provide usage analytics, and pay for the service

The service will intentionally be built without irrelevant modules, such as host, renter, or miner, in order to avoid possible conflicts of interests.
When the service is fully developed, anyone will be able to run their own satellite. This way, the centralization piece can be mitigated.


This work is supported by a [Sia Foundation](https://sia.tech) grant.
