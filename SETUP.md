# How to Setup a Satellite Node

This guide will walk you through the process of setting up a Satellite node.

## What You Will Need

You will need a server with the root access (directly or over SSH). The blockchain is quite large (over 40GB at the moment of writing), so you need to account for that. It is recommended to use an SSD, because it will affect the syncing speed.

This guide will assume that you use Ubuntu Server 22.04 LTS. If you run any other OS, the setup process may differ.

You will need a fully registered domain name. This guide will use `your_domain` as the domain name. DNS A records with both `your_domain` and `www.your_domain` need to be pointing to your server's public IP address.

You will need a Stripe account to accept payments.

You will also need an email account, from which the users will be receiving emails with the verification and password reset links.

## To Start With

Log into your server and download the Satellite files. This guide assumes that you will use the version `0.8.0` for an x86 CPU:
```
mkdir ~/satellite
cd ~/satellite
wget -q https://github.com/mike76-dev/satellite/releases/download/v0.8.0/satellite_linux_amd64.zip
unzip satellite_linux_amd64.zip
rm satellite_linux_amd64.zip
```

## Installing HTTP Server

This guide will use Apache2 as the HTTP server. You may choose another server, then the process will differ.

Begin by updating the local package index to reflect the latest upstream changes:
```
$ sudo apt update
```
Then, install the `apache2` package:
```
$ sudo apt install apache2
```
After confirming the installation, apt will install Apache and all required dependencies.

Now, you need to configure the firewall to allow outside access to the default web ports.
During installation, Apache registers itself with UFW to provide a few application profiles that can be used to enable or disable access to Apache through the firewall.

List the `ufw` application profiles by running the following:
```
$ sudo ufw app list
```
Your output will be a list of the application profiles:
```
Output:
Available applications:
  Apache
  Apache Full
  Apache Secure
  OpenSSH
```
If you haven't done so yet, allow SSH connections to your server:
```
$ sudo ufw allow 'OpenSSH'
```
And enable your firewall:
```
$ sudo ufw enable
```
Since you haven’t configured SSL for your server yet in this guide, you’ll only need to allow traffic on port 80:
```
$ sudo ufw allow 'Apache'
```
You can verify the change by checking the status:
```
$ sudo ufw status
```
The output will provide a list of allowed HTTP traffic:
```
Output:
Status: active

To                         Action      From
--                         ------      ----
OpenSSH                    ALLOW       Anywhere
Apache                     ALLOW       Anywhere
OpenSSH (v6)               ALLOW       Anywhere (v6)
Apache (v6)                ALLOW       Anywhere (v6)
```
Make sure the service is active by running the command for the systemd init system:
```
$ sudo systemctl status apache2
```
```
Output:
● apache2.service - The Apache HTTP Server
     Loaded: loaded (/lib/systemd/system/apache2.service; enabled; vendor prese>
     Active: active (running) since Sun 2023-04-16 20:25:40 UTC; 43s ago
       Docs: https://httpd.apache.org/docs/2.4/
   Main PID: 15430 (apache2)
      Tasks: 55 (limit: 38110)
     Memory: 5.1M
        CPU: 58ms
     CGroup: /system.slice/apache2.service
             ├─15430 /usr/sbin/apache2 -k start
             ├─15433 /usr/sbin/apache2 -k start
             └─15434 /usr/sbin/apache2 -k start
```
Apache on Ubuntu 22.04 has one server block enabled by default that is configured to serve documents from the /var/www/html directory. While this works well for a single site, it is recommended to create a directory structure within /var/www for a your_domain site, leaving /var/www/html in place as the default directory to be served if a client request doesn’t match any other sites.

Create the directory for `your_domain` as follows:
```
$ sudo mkdir /var/www/your_domain
```
Next, assign ownership of the directory to the user you’re currently signed in as with the `$USER` environment variable:
```
$ sudo chown -R $USER:$USER /var/www/your_domain
```
The permissions of your web root should be correct if you haven’t modified your umask value, which sets default file permissions. To ensure that your permissions are correct and allow the owner to read, write, and execute the files while granting only read and execute permissions to groups and others, you can input the following command:
```
$ sudo chmod -R 755 /var/www/your_domain
```

## Enabling TLS/SSL

Now let's enable HTTPS on your server by installing a TLS/SSL certificate from Let's Encrypt.

First, run the following to install the Certbot software:
```
$ sudo apt install certbot python3-certbot-apache
```
Then edit the virtual host file for your domain:
```
$ sudo nano /etc/apache2/sites-available/your_domain.conf
```
Add in the following configuration block, which is similar to the default, but updated for your new directory and domain name:
```
<VirtualHost *:80>
    ServerAdmin webmaster@localhost
    ServerName your_domain
    ServerAlias www.your_domain
    DocumentRoot /var/www/your_domain
    ErrorLog ${APACHE_LOG_DIR}/error.log
    CustomLog ${APACHE_LOG_DIR}/access.log combined
</VirtualHost>
```
Save and close the file when you are finished.

Now enable the file with the `a2ensite` tool:
```
$ sudo a2ensite your_domain.conf
```
Disable the default site defined in `000-default.conf`:
```
$ sudo a2dissite 000-default.conf
```
Next, test for configuration errors:
```
$ sudo apache2ctl configtest
```
You should receive `Syntax OK` as a response. If you get an error, reopen the virtual host file and check for any typos or missing characters. Once your configuration file’s syntax is correct, restart Apache so that the changes take effect:
```
$ sudo systemctl restart apache2
```
With these changes, Certbot will be able to find the correct VirtualHost block and update it.

Next, you’ll update the firewall to allow HTTPS traffic. For this, allow the “Apache Full” profile:
```
$ sudo ufw allow 'Apache Full'
```
Then delete the redundant “Apache” profile:
```
$ sudo ufw delete allow 'Apache'
```
Your status will display as the following:
```
$ sudo ufw status
```
```
Output:
Status: active

To                         Action      From
--                         ------      ----
OpenSSH                    ALLOW       Anywhere
Apache Full                ALLOW       Anywhere
OpenSSH (v6)               ALLOW       Anywhere (v6)
Apache Full (v6)           ALLOW       Anywhere (v6)
```
You are now ready to run Certbot and obtain your certificates.

Certbot provides a variety of ways to obtain SSL certificates through plugins. The Apache plugin will take care of reconfiguring Apache and reloading the configuration whenever necessary. To use this plugin, run the following:
```
$ sudo certbot --apache
```
This script will prompt you to answer a series of questions in order to configure your SSL certificate. First, it will ask you for a valid email address. This email will be used for renewal notifications and security notices:
```
Output:
Saving debug log to /var/log/letsencrypt/letsencrypt.log
Enter email address (used for urgent renewal and security notices)
 (Enter 'c' to cancel): you@your_domain
 ```
After providing a valid email address, press `ENTER` to proceed to the next step. You will then be prompted to confirm if you agree to Let’s Encrypt terms of service. You can confirm by pressing `Y` and then `ENTER`:
```
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
Please read the Terms of Service at
https://letsencrypt.org/documents/LE-SA-v1.2-November-15-2017.pdf. You must
agree in order to register with the ACME server at
https://acme-v02.api.letsencrypt.org/directory
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
(Y)es/(N)o: Y
```
Next, you’ll be asked if you would like to share your email with the Electronic Frontier Foundation to receive news and other information. If you do not want to subscribe to their content, write `N`. Otherwise, write `Y` then press `ENTER` to proceed to the next step:
```
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
Would you be willing to share your email address with the Electronic Frontier
Foundation, a founding partner of the Let's Encrypt project and the non-profit
organization that develops Certbot? We'd like to send you email about our work
encrypting the web, EFF news, campaigns, and ways to support digital freedom.
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
(Y)es/(N)o: N
```
The next step will prompt you to inform Certbot of which domains you’d like to activate HTTPS for. The listed domain names are automatically obtained from your Apache virtual host configuration, so it’s important to make sure you have the correct `ServerName` and `ServerAlias` settings configured in your virtual host. If you’d like to enable HTTPS for all listed domain names (recommended), you can leave the prompt blank and press `ENTER` to proceed. Otherwise, select the domains you want to enable HTTPS for by listing each appropriate number, separated by commas and/ or spaces, then press `ENTER`:
```
Which names would you like to activate HTTPS for?
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
1: your_domain
2: www.your_domain
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
Select the appropriate numbers separated by commas and/or spaces, or leave input
blank to select all options shown (Enter 'c' to cancel):
```
After this step, Certbot’s configuration is finished, and you will be presented with the final remarks about your new certificate and where to locate the generated files:
```
Output:
Successfully received certificate.
Certificate is saved at: /etc/letsencrypt/live/your_domain/fullchain.pem
Key is saved at:         /etc/letsencrypt/live/your_domain/privkey.pem
This certificate expires on 2023-07-16.
These files will be updated when the certificate renews.
Certbot has set up a scheduled task to automatically renew this certificate in the background.

Deploying certificate
Successfully deployed certificate for your_domain to /etc/apache2/sites-available/your_domain-le-ssl.conf
Successfully deployed certificate for www.your_domain to /etc/apache2/sites-available/your_domain-le-ssl.conf
Congratulations! You have successfully enabled HTTPS on https://your_domain and https://www.your_domain

- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
If you like Certbot, please consider supporting our work by:
 * Donating to ISRG / Let's Encrypt:   https://letsencrypt.org/donate
 * Donating to EFF:                    https://eff.org/donate-le
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
```
Your certificate is now installed and loaded into Apache’s configuration.

## Configuring Reverse Proxy

You need to configure a reverse proxy for the portal API to work. To do this, first enable some additional Apache modules:
```
$ sudo a2enmod proxy proxy_http proxy_balancer lbmethod_byrequests
```
Open the SSL-enabled virtual host file for your domain and add the following lines just before the closing `</VirtualHost>` tag:
```
$ sudo nano /etc/apache2/sites-available/your_domain-le-ssl.conf
```
```
ProxyPreserveHost On
ProxyPass /api http://127.0.0.1:8080
ProxyPassReverse /api http://127.0.0.1:8080/api
```
Here, `8080` will be the port that the portal API will listen on. You can choose any other port, but remember to put it in the config file later on.

To put these changes into effect, restart Apache:
```
$ sudo systemctl restart apache2
```

## Installing MySQL

Sia Satellite uses a MySQL database to store the metadata. To install MySQL on your server, run this:
```
$ sudo apt install mysql-server
```
Ensure that the server is running using the `systemctl start` command:
```
$ sudo systemctl start mysql.service
```
These commands will install and start MySQL, but will not prompt you to set a password or make any other configuration changes. Because this leaves your installation of MySQL insecure, we will address this next. First, open up the MySQL prompt:
```
$ sudo mysql
```
Then run the following `ALTER USER` command to change the root user’s authentication method to one that uses a password. The following example changes the authentication method to `mysql_native_password`:
```
mysql> ALTER USER 'root'@'localhost' IDENTIFIED WITH mysql_native_password BY 'password';
```
After making this change, exit the MySQL prompt:
```
mysql> exit;
```
Run the security script with `sudo`:
```
$ sudo mysql_secure_installation
```
This will take you through a series of prompts where you can make some changes to your MySQL installation’s security options. The first prompt will ask whether you’d like to set up the Validate Password Plugin, which can be used to test the password strength of new MySQL users before deeming them valid.

If you elect to set up the Validate Password Plugin, any MySQL user you create that authenticates with a password will be required to have a password that satisfies the policy you select:
```
Output:
Securing the MySQL server deployment.

Connecting to MySQL using a blank password.

VALIDATE PASSWORD COMPONENT can be used to test passwords
and improve security. It checks the strength of password
and allows the users to set only those passwords which are
secure enough. Would you like to setup VALIDATE PASSWORD component?

Press y|Y for Yes, any other key for No: Y
```
```
Output:
There are three levels of password validation policy:

LOW    Length >= 8
MEDIUM Length >= 8, numeric, mixed case, and special characters
STRONG Length >= 8, numeric, mixed case, special characters and dictionary file

Please enter 0 = LOW, 1 = MEDIUM and 2 = STRONG:
 2
```
If you used the Validate Password Plugin, you’ll receive feedback on the strength of your new password. Then the script will ask if you want to continue with the password you just entered or if you want to enter a new one. Assuming you’re satisfied with the strength of the password you just entered, enter `Y` to continue the script:
```
Output:
Estimated strength of the password: 100
Do you wish to continue with the password provided?(Press y|Y for Yes, any other key for No) : Y
```
From there, you can press `Y` and then `ENTER` to accept the defaults for all the subsequent questions. This will remove some anonymous users and the test database, disable remote root logins, and load these new rules so that MySQL immediately respects the changes you have made.

Once the security script completes, you can then reopen MySQL and change the root user’s authentication method back to the default, `auth_socket`. To authenticate as the root MySQL user using a password, run this command:
```
$ mysql -u root -p
```
Then go back to using the default authentication method using this command:
```
mysql> ALTER USER 'root'@'localhost' IDENTIFIED WITH auth_socket;
```
This will mean that you can once again connect to MySQL as your root user using the `sudo mysql` command.
```
$ sudo mysql
```
Allow the root user to grant privileges:
```
mysql> GRANT ALL PRIVILEGES ON *.* TO 'root'@'localhost' WITH GRANT OPTION;
mysql> FLUSH PRIVILEGES;
```
Run the following command to create a database for the Satellite. This guide will be using `satellite` as the database name. Take a note of this name:
```
mysql> CREATE DATABASE satellite;
```
Then create a user for the Satellite. This guide will be using `satuser` as the user name. Take a note of this name and be sure to change password to a strong password of your choice:
```
mysql> CREATE USER 'satuser'@'localhost' IDENTIFIED WITH mysql_native_password BY 'password';
```
Now grant this user access to the database:
```
mysql> GRANT ALL PRIVILEGES ON satellite.* TO 'satuser'@'localhost';
mysql> FLUSH PRIVILEGES;
```
Exit MySQL and log in as `satuser`:
```
mysql> exit;
$ cd ~/satellite
$ mysql -u satuser -p
```
Create the database tables:
```
mysql> USE satellite;
mysql> SOURCE init.sql;
```
Now exit MySQL:
```
mysql> exit;
```

## Setting Up a Stripe Account

Unfortunately, this is the least straightforward part of the setup process, because a lot depends on how Stripe looks at your business, and they may even suspend or close your account if they find that you are violating certain policies.

In general, it is recommended to respond to any requests as soon as possible. If you receive a closure notice, that may be because you are suspected in conducting forbidden activities, e.g. mining or trading crypto currencies. Object that the Satellite is used solely for selling cloud storage for fiat money, so the customers don't buy crypto with their credit cards.

Once you have an account with Stripe, go to the "Developers" section of the Dashboard. You need to do three things there:
1. Switch off the "Test mode" (the slider on the right-hand side).
2. Go to the "API keys" tab and request the Publishable key and the Secret key. Take a note of both.
3. Go to the "Webhooks" tab and add a new endpoint with the URL `https://your_domain/api/stripe/webhook` listening for `payment_intent.succeeded` and `payment_intent.payment_failed` events. Reveal the signing secret and take a note of it.

## Checking the Siacoin Exchange Rate API

Sia Satellite uses the Bittrex API to fetch the SC/USD exchange rate. To check if it works from your server's location, enter this:
```
$ curl https://api.bittrex.com/v3/markets/SC-USD/ticker
```
This should produce an output similar to the following:
```
Output:
{"symbol":"SC-USD","lastTradeRate":"0.004290000000","bidRate":"0.004180000000","askRate":"0.004260000000","updatedAt":"2023-04-17T11:09:23.3590326Z"}
```
If this API is unavailable at your server's location, let me know and I will add another option for you.

## Configuring the Currency Exchange API

Sia Satellite uses Freecurrency API to fetch the exchange rates of different fiat currencies to US dollar. Freecurrency has a free tier but requires an authentication to prevent an abuse. You need to sign up at `https://freecurrencyapi.com` and receive an API key.

## Configuring SATD

Open the `satdconfig.json` file:
```
$ nano satdconfig.json
```
First, choose a `name` of your satellite node (Hint: it makes sense to use your domain name for it). This is required to receive email reports later on. Fill in the `dbUser` and `dbName` fields with the MySQL user name (`satuser`) and the database name (`satellite`). Set the directory to store the `satd` metadata and log files (here it is `/usr/local/etc/satd`). You can also change the default portal API port number (`:8080`) as well as the other ports:
```
"Satd Configuration"
"0.3.0"
{
  "name": "your_chosen_name",
	"agent": "Sat-Agent",
	"gateway": ":0",
	"api": "localhost:9990",
	"satellite": ":9992",
  "mux": ":9993",
	"dir": "/usr/local/etc/satd",
	"bootstrap": true,
	"dbUser": "satuser",
	"dbName": "satellite",
	"portal": ":8080"
}
```
Save and exit. Now copy the file to its new location:
```
$ sudo mkdir /usr/local/etc/satd
$ sudo cp satdconfig.json /usr/local/etc/satd
```
Open the `mail.json` file:
```
$ nano mail.json
```
Fill in the fields. Replace `your_email_address` with the email address that will be used to send out the links, and `SMTP_server` and `SMTP_port` with the address and the port number of the SMTP server, respectively:
```
"smtp"
{
	"from": "your_email_address",
	"host": "SMTP_server",
	"port": "SMTP_port"
}
```
Save and exit. Copy the file to the location specified earlier under 'dir' in 'satdconfig.json':
```
$ sudo cp mail.json /usr/local/etc/satd
```
Change the ownership of the config files. Replace `<user>` with the name of the user that will be running the Satellite:
```
$ sudo chown <user> /usr/local/etc/satd
$ sudo chown <user> /usr/local/etc/satd/*
```
Now copy the binaries over:
```
$ sudo cp satd /usr/local/bin
$ sudo cp satc /usr/local/bin
```
The easiest way to run a Satellite node is via `systemd`. For this, create the service file:
```
$ sudo nano /etc/systemd/system/satd.service
```
Enter the following lines. Replace:
`<user>` with the name of the user that will be running `satd`,
`<api_password>` with the `satd` API password of your choice,
`<db_password>` with the MySQL user password created earlier,
`<wallet_password>` with the wallet encryption password that you will create at a later step,
`<mail_password>` with your SMTP server authentication password,
`<freecurrency_key>` with your Freecurrency API key,
`<stripe_key>` with your Stripe secret key,
`<webhook_key>` with your Stripe webhook signing secret.
```
[Unit]
Description=satd
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/satd --dir=/usr/local/etc/satd
ExecStop=/usr/local/bin/satc stop
TimeoutStopSec=660
Restart=always
RestartSec=15
User=<user>
Environment="SATD_API_PASSWORD=<api_password>"
Environment="SATD_DB_PASSWORD=<db_password>"
Environment="SATD_WALLET_PASSWORD=<wallet_password>"
Environment="SATD_CONFIG_DIR=/usr/local/etc/satd"
Environment="SATD_MAIL_PASSWORD=<mail_password>"
Environment="SATD_FREECURRENCY_API_KEY=<freecurrency_key>"
Environment="SATD_STRIPE_KEY=<stripe_key>"
Environment="SATD_STRIPE_WEBHOOK_KEY=<webhook_key>"
LimitNOFILE=900000

[Install]
WantedBy=multi-user.target
Alias=satd.service
```
Save and exit.

One last thing before you start the server, open the Provider port:
```
$ sudo ufw allow 9992
```
```
Output:
Rule added
Rule added (v6)
```
Now you are ready to start the Satellite:
```
$ sudo systemctl start satd
```
Open the `systemd` journal to see the log:
```
$ journalctl -u satd -f
```
If everything went well, you should see the following output:
```
Output:
Nov 18 15:16:17 <host> systemd[1]: Started satd.
Nov 18 15:16:17 <host> satd[226480]: Using SATD_CONFIG_DIR environment variable to load config.
Nov 18 15:16:17 <host> satd[226480]: Using SATD_API_PASSWORD environment variable.
Nov 18 15:16:17 <host> satd[226480]: Using SATD_DB_PASSWORD environment variable.
Nov 18 15:16:17 <host> satd[226480]: satd v0.8.0
Nov 18 15:16:17 <host> satd[226480]: Git Revision 2f5a918
Nov 18 15:16:17 <host> satd[226480]: Loading...
Nov 18 15:16:17 <host> satd[226480]: Creating mail client...
Nov 18 15:16:17 <host> satd[226480]: Connecting to the SQL database...
Nov 18 15:16:17 <host> satd[226480]: Loading gateway...
Nov 18 15:16:17 <host> satd[226480]: Loading consensus...
Nov 18 15:16:17 <host> satd[226480]: Loading transaction pool...
Nov 18 15:16:17 <host> satd[226480]: Loading wallet...
Nov 18 15:16:17 <host> satd[226480]: Loading manager...
Nov 18 15:16:18 <host> satd[226480]: Loading provider...
Nov 18 15:16:18 <host> satd[226480]: Loading portal...
Nov 18 15:16:18 <host> satd[226480]: API is now available, synchronous startup completed in 0.019 seconds
Nov 18 15:16:18 <host> satd[226480]: Wallet Password found, attempting to auto-unlock wallet...
Nov 18 15:16:18 <host> satd[226480]: Auto-unlock failed: provided encryption key is incorrect
Nov 18 15:16:18 <host> satd[226480]: Finished full setup in 0s
```
The daemon will now be syncing to the blockchain. You can monitor the progress with the following command:
```
$ satc consensus --apipassword <api_password>
```
```
Output:
Synced: No
Height: 37370
```
Once the node is synced, the output will change:
```
Output:
Synced: Yes
Block:      bid:00000000000000000b9b687a2794ff4dd450bfed6f7fc452e27bcf2a433d8d93
Height:     444562
Target:     [0 0 0 0 0 0 0 1 35 47 52 114 83 128 110 166 227 1 23 115 197 236 127 4 15 91 59 30 228 136 86 160]
Difficulty: ~16.22 uS
```
You should now create a wallet. You will need the password you set in your `systemd` unit earlier. When prompted type in the wallet password you chose.
```
$ satc wallet init -p --apipassword <api_password>
```
A new 12-word wallet seed will be generated. Save this seed somewhere secure.
```
Output:
Wallet password: 
Confirm: 
Recovery seed:
<a_new_wallet_seed>

Wallet encrypted with given password
```
You should now unlock your wallet. In the future, when starting Satellite your wallet will automatically unlock, this is why we put the wallet password in the `systemd` unit.
```
$ satc wallet unlock --apipassword <api_password>
```
If you enter
```
$ satc wallet --apipassword <api_password>
```
now, you should see the following output:
```
Output:
Wallet status:
Encrypted, Unlocked
Height:              18059
Confirmed Balance:   0 H
Unconfirmed Delta:   +0 H
Exact:               0 H
Estimated Fee:       30 mS / KB
```
The last step is to generate an address to send Siacoin to:
```
$ satc wallet address
```
```
Output:
Created new address: <address>
```
You can now send Siacoin to this address to fund the Satellite, however the funds will not show up until you are fully synced.

Last, you may want to sign up for receiving monthly email reports as well as alerts about the low wallet balance. To do this, run
```
$ satc manager setpreferences <your_email_address> 1KS --apipassword <api_password>
```
Here, `1KS` is the balance threshold, after which you will be receiving daily email alerts. You can choose any other value. If you want to unsubscribe, run
```
$ satc manager setpreferences none 0 --apipassword <api_password>
```

## Setting Up the Web Portal

You need to do some changes to certain pages of the portal.

`~/satellite/webportal/about.html` should contain the information about your node and you as the operator. The specific information you need to provide depends on your and your server's jurisdiction.

`~/satellite/webportal/privacy.html` should include the information about what user data the site collects and how it is used.

`~/satellite/webportal/tos.html` should include any legal provisions applicable to your and your server's jurisdiction. It is probably better to consult a lawyer on how to put it together.

Now edit the config file. Put `/api` in `apiBaseURL` and your Stripe API publishable key in `stripePublicKey`:

```
$ nano webportal/js/config.js
```

```
// Change this to the network path your API server is listening at.
const apiBaseURL = '/api';

// This is your publishable Stripe API key.
const stripePublicKey = '<Stripe_PK>';
```

Save and exit. Now copy the entire contents of `~/satellite/webportal` to where the root documents of your HTTP server need to be:

```
$ sudo cp -r webportal/* /var/www/your_domain/
```

Now open your browser and visit `https://your_domain`. If everything went well, you will see the portal page.

Congratulations, you have successfully set up your Satellite node!
