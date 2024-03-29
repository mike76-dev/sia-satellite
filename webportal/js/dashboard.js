if (apiBaseURL == '') {
	throw new Error('API base URL not specified');
}

const month = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

if (!navigator.cookieEnabled || getCookie('satellite') == '') {
	let i = window.location.href.lastIndexOf('/');
	window.location.replace(window.location.href.slice(0, i) + '/rent.html');
}

function getRemSize() {
	return parseInt(getComputedStyle(document.getElementsByTagName('body')[0]).fontSize);
}

function getCookie(name) {
	let n = name + '=';
	let ca = document.cookie.split(';');
	let c;
	for (let i = 0; i < ca.length; i++) {
		c = ca[i];
		while (c.charAt(0) === ' ') c = c.substring(1, c.length);
		if (c.indexOf(n) === 0) {
			return c.substring(n.length, c.length);
		}
	}
	return '';
}

function deleteCookie(name) {
	document.cookie = name + '=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/';
}

var userData = {
	paymentsStep:     10,
	contractsStep:    10,
	currentContracts: false,
	oldContracts:     false,
	sortByStart:      'inactive',
	sortByEnd:        'inactive',
	sortByTimestamp:  'ascending',
	activeMenuIndex:  0,
	encryptionKey:    null,
}

var menu = document.getElementById('menu');
var pages = document.getElementById('pages');
for (let i = 0; i < menu.childElementCount; i++) {
	menu.children[i].addEventListener('click', function(e) {
		setActiveMenuIndex(e.target.getAttribute('index'));
	});
}

var averages = {
	currency: 'USD',
	numHosts: 0,
	duration: '',
	storagePrice: 0.0,
	collateral: 0.0,
	downloadBandwidthPrice: 0.0,
	uploadBandwidthPrice: 0.0,
	contractPrice: 0.0,
	baseRPCPrice: 0.0,
	sectorAccessPrice: 0.0,
	rate: 0.0
}

var payments = [];
var paymentsFrom = 1;
var contracts = [];
var contractsFrom = 1;
var files = [];
var selectedFiles = [];
var downloads = [];

var expandedContract = -1;

var currencies = [];
var paymentEstimation;
var paymentAmount = 0.0;
var paymentCurrency = 'USD';
var processing = false;
var paying = false;
var refreshing = false;

var keyValue = '';

var buckets = [];
var currentBucket = '';
var currentPath = '';

loadFromStorage();
getVersion();
getCurrencies();
retrieveBlockHeight();
retrieveBalance();
retrieveAverages();
retrieveContracts();
retrieveFiles();
var blockHeightInterval = window.setInterval(retrieveBlockHeight, 30000);
var balanceInterval = window.setInterval(retrieveBalance, 30000);
var averagesInterval = window.setInterval(retrieveAverages, 600000);
var contractsInterval = window.setInterval(retrieveContracts, 300000);
var filesInterval = window.setInterval(retrieveFiles, 30000);
var refreshInterval = window.setInterval(refresh, 30000);
retrieveKey();

function loadFromStorage() {
	let data = window.localStorage.getItem('userData');
	if (data) userData = JSON.parse(data);
	document.getElementById('history-rows').value = userData.paymentsStep;
	document.getElementById('contracts-rows').value = userData.contractsStep;
	if (userData.currentContracts) document.getElementById('contracts-current').checked = true;
	if (userData.oldContracts) document.getElementById('contracts-old').checked = true;
	if (userData.sortByStart == 'ascending') document.getElementById('contracts-start-asc').classList.add('active');
	if (userData.sortByStart == 'descending') document.getElementById('contracts-start-desc').classList.add('active');
	if (userData.sortByEnd == 'ascending') document.getElementById('contracts-end-asc').classList.add('active');
	if (userData.sortByEnd == 'descending') document.getElementById('contracts-end-desc').classList.add('active');
	if (userData.sortByTimestamp == 'ascending') {
		document.getElementById('history-timestamp-asc').classList.add('active');
		document.getElementById('history-timestamp-desc').classList.remove('active');
	}
	if (userData.sortByTimestamp == 'descending') {
		document.getElementById('history-timestamp-desc').classList.add('active');
		document.getElementById('history-timestamp-asc').classList.remove('active');
	}
	if (userData.encryptionKey) {
		let key = Array.from(userData.encryptionKey, ((byte) => ('0' + (byte & 0xFF).toString(16)).slice(-2))).join('');
		document.getElementById('files-key').value = key;
		changeEncryptionKey();
	}
	setActiveMenuIndex(userData.activeMenuIndex);
}

function refresh() {
	refreshing = true;
	setActiveMenuIndex(userData.activeMenuIndex);
	refreshing = false;
}

function setActiveMenuIndex(ind) {
	let li, p;
	if (ind > menu.childElementCount) return;
	userData.activeMenuIndex = ind;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	for (let i = 0; i < menu.childElementCount; i++) {
		li = menu.children[i];
		p = pages.children[i];
		if (i == ind) {
			li.classList.add('active');
			p.classList.remove('disabled');
		} else {
			li.classList.remove('active');
			p.classList.add('disabled');
		}
	}
	document.getElementById('menu-button').classList.remove('mobile-hidden');
	document.getElementById('menu-container').classList.add('mobile-hidden');
	clearErrors();
	if (ind == 1) {
		getContracts();
	}
	if (ind == 2) {
		getFiles();
	}
	if (ind == 3) {
		getSpendings();
	}
	if (ind == 4) {
		document.getElementById('sc-generate').innerText = 'Show Address';
		document.getElementById('sc-address').value = '';
	}
	if (ind == 5) {
		getPayments();
	}
	if (ind == 6) {
		getSettings();
	}
}

function showMenu(e) {
	e.preventDefault();
	e.stopPropagation();
	document.getElementById('menu-button').classList.add('mobile-hidden');
	document.getElementById('menu-container').classList.remove('mobile-hidden');
	document.addEventListener('click', documentClickHandler);
}

function documentClickHandler() {
	document.removeEventListener('click', documentClickHandler);
	document.getElementById('menu-button').classList.remove('mobile-hidden');
	document.getElementById('menu-container').classList.add('mobile-hidden');
}

function toggleChangePassword() {
	let c = document.getElementById('change-password-toggle');
	let i = document.getElementById('change-password-icon');
	let p = document.getElementById('change-password');
	if (c.checked) {
		p.type = 'text';
		i.src = 'assets/hide-password.png';
	} else {
		p.type = 'password';
		i.src = 'assets/show-password.png';
	}
}

function toggleChangeRetype() {
	let c = document.getElementById('change-retype-toggle');
	let i = document.getElementById('change-retype-icon');
	let p = document.getElementById('change-retype');
	if (c.checked) {
		p.type = 'text';
		i.src = 'assets/hide-password.png';
	} else {
		p.type = 'password';
		i.src = 'assets/show-password.png';
	}
}

function clearErrors() {
	document.getElementById('change-password-error').classList.add('invisible');
	document.getElementById('change-retype-error').classList.add('invisible');
}

function changePasswordChange() {
	let err = document.getElementById('change-password-error');
	err.classList.add('invisible');
}

function changeRetypeChange() {
	let err = document.getElementById('change-retype-error');
	err.classList.add('invisible');
}

function clearPassword() {
	document.getElementById('change-password').value = '';
	document.getElementById('change-retype').value = '';
}

function changeClick() {
	let p = document.getElementById('change-password');
	if (p.value.length < 8) {
		let err = document.getElementById('change-password-error');
		err.innerHTML = 'Provided password is too short';
		err.classList.remove('invisible');
		return;
	}
	if (p.value.length > 255) {
		let err = document.getElementById('change-password-error');
		err.innerHTML = 'Provided password is too long';
		err.classList.remove('invisible');
		return;
	}
	let r = document.getElementById('change-retype');
	if (r.value != p.value) {
		let err = document.getElementById('change-retype-error');
		err.innerHTML = 'The two passwords do not match';
		err.classList.remove('invisible');
		return;
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type':       'application/json;charset=utf-8',
			'Satellite-Password': p.value
		}
	}
	let m = document.getElementById('message');
	fetch(apiBaseURL + '/auth/change', options)
		.then(response => {
			if (response.status == 204) {
				clearPassword();
				m.innerHTML = 'Password changed successfully...';
				m.classList.remove('disabled');
				window.setTimeout(function() {
					m.classList.add('disabled');
					m.innerHTML = '';
				}, 3000);
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let passErr = document.getElementById('change-password-error');
			switch (data.code) {
				case 20:
					passErr.innerHTML = 'Password is too short';
					passErr.classList.remove('invisible');
					break;
				case 21:
					passErr.innerHTML = 'Password is too long';
					passErr.classList.remove('invisible');
					break;
				case 40:
					clearPassword();
					m.innerHTML = 'Unknown error. Recommended to clear the cookies and reload the page.';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
					break;
				case 41:
					clearPassword();
					m.innerHTML = 'Unknown error. Recommended to clear the cookies and reload the page.';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
					break;
				case 50:
					emailErr.innerHTML = 'Unknown error';
					emailErr.classList.remove('invisible');
					break;
				default:
			}
		})
		.catch(error => console.log(error));
}

function deleteClick() {
	if (!confirm('Are you sure you want to delete your account?')) return;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	let m = document.getElementById('message');
	fetch(apiBaseURL + '/auth/delete', options)
		.then(response => {
			if (response.status == 204) {
				logout();
			} else return response.json();
		})
		.then(data => {
			switch (data.code) {
				case 2:
					m.innerHTML = 'Your account balance is negative. Please refill your account balance and try again.';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
					break;
				case 40:
					m.innerHTML = 'Unknown error. Recommended to clear the cookies and reload the page.';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
					break;
				case 41:
					m.innerHTML = 'Unknown error. Recommended to clear the cookies and reload the page.';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
					break;
				case 50:
					m.innerHTML = 'Unknown error';
					m.classList.remove('disabled');
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
					}, 3000);
				default:
			}
		})
		.catch(error => console.log(error));
}

function logout() {
	deleteCookie('satellite');
	window.localStorage.removeItem('userData');
	window.clearInterval(blockHeightInterval);
	window.clearInterval(balanceInterval);
	window.clearInterval(averagesInterval);
	window.clearInterval(contractsInterval);
	window.clearInterval(filesInterval);
	window.clearInterval(refreshInterval);
	let i = window.location.href.lastIndexOf('/');
	window.location.replace(window.location.href.slice(0, i) + '/rent.html');
}

function retrieveBalance() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/balance', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				if (data.code == 40) {
					logout();
				} else {
					console.log(data);
				}
			}
			else {
				document.getElementById('overview-email').innerHTML = '<strong>' + data.email + '</strong>';
				document.getElementById('overview-status').innerHTML = data.isrenter ? 'complete' :
					'<span style="cursor: pointer" onclick="setActiveMenuIndex(4)">incomplete</span>';
				document.getElementById('overview-plan').innerHTML =
					(data.subscribed ? 'invoicing' : 'pre-payment');
				let bp = document.getElementById('balance-primary');
				let bs = document.getElementById('balance-secondary');
				let lp = document.getElementById('locked-primary');
				let ls = document.getElementById('locked-secondary');
				let sc = document.getElementById('select-change');
				let c = data.currency == '' ? 'USD' : data.currency;
				bp.innerHTML = (data.balance * data.scrate).toFixed(2) + ' ' + c;
				bs.innerHTML = data.balance.toFixed(2) + ' SC';
				lp.innerHTML = (data.locked * data.scrate).toFixed(2) + ' ' + c;
				ls.innerHTML = data.locked.toFixed(2) + ' SC';
				paymentCurrency = c;
				if (averages.currency != c) {
					averages.currency = c;
					retrieveAverages();
				}
				document.getElementById('select-plan').innerHTML =
					(data.subscribed ? 'invoicing' : 'pre-payment');
				document.getElementById('select-balance-primary').innerHTML = 
					(data.balance * data.scrate).toFixed(2) + ' ' + c;
				document.getElementById('select-balance-secondary').innerHTML = 
					data.balance.toFixed(2) + ' SC';
				document.getElementById('select-currency').value = c;
				document.getElementById('payment-currency').innerHTML = c;
				document.getElementById('contract-currency').innerHTML = c;
				document.getElementById('storage-currency').innerHTML = c;
				document.getElementById('upload-currency').innerHTML = c;
				document.getElementById('download-currency').innerHTML = c;
				if (!paying) {
					document.getElementById('select').classList.remove('disabled');
				}
				if (data.isrenter) {
					document.getElementById('reveal').classList.remove('disabled');
					document.getElementById('opt-in').classList.remove('disabled');
				}
				if ((data.subscribed || data.isrenter) && data.stripeid != '') {
					sc.classList.remove('disabled');
				}
				sc.innerHTML = data.subscribed ? 'Switch to Pre-Payment' : 'Switch to Invoicing';
			}
		})
		.catch(error => console.log(error));
}

function retrieveAverages() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/averages?currency=' + averages.currency, options)
		.then(response => response.json())
		.then(data => {
			if (data.code) console.log(data)
			else {
				averages.numHosts = data.numhosts;
				averages.duration = data.duration;
				averages.storagePrice = data.storageprice;
				averages.collateral = data.collateral;
				averages.downloadBandwidthPrice = data.downloadbandwidthprice;
				averages.uploadBandwidthPrice = data.uploadbandwidthprice;
				averages.contractPrice = data.contractprice;
				averages.baseRPCPrice = data.baserpcprice;
				averages.sectorAccessPrice = data.sectoraccessprice;
				averages.rate = data.scrate;

				document.getElementById('numhosts').innerHTML = data.numhosts;
				document.getElementById('storage-primary').innerHTML =
					(data.storageprice * data.scrate).toFixed(2) + ' ' + averages.currency;
				document.getElementById('storage-secondary').innerHTML =
					data.storageprice.toFixed(0) + ' SC';
				document.getElementById('upload-primary').innerHTML =
					(data.uploadbandwidthprice * data.scrate).toFixed(2) +' ' + averages.currency;
				document.getElementById('upload-secondary').innerHTML =
					data.uploadbandwidthprice.toFixed(0) +' SC';
				document.getElementById('download-primary').innerHTML =
					(data.downloadbandwidthprice * data.scrate).toFixed(2) + ' ' + averages.currency;
				document.getElementById('download-secondary').innerHTML =
					data.downloadbandwidthprice.toFixed(0) + ' SC';
				document.getElementById('duration-primary').innerHTML = blocksToTime(data.duration);
				document.getElementById('duration-secondary').innerHTML = data.duration + ' blocks';
				calcAverages();
			}
		})
		.catch(error => console.log(error));
}

function calcAverages() {
	document.getElementById('limits-contract-average').innerHTML = 'Average: ' +
		(averages.contractPrice * averages.rate).toPrecision(2) + ' ' + averages.currency;
	document.getElementById('limits-storage-average').innerHTML = 'Average: ' +
		(averages.storagePrice * averages.rate).toPrecision(2) + ' ' + averages.currency;
	document.getElementById('limits-upload-average').innerHTML = 'Average: ' +
		(averages.uploadBandwidthPrice * averages.rate).toPrecision(2) + ' ' + averages.currency;
	document.getElementById('limits-download-average').innerHTML = 'Average: ' +
		(averages.downloadBandwidthPrice * averages.rate).toPrecision(2) + ' ' + averages.currency;
}

function blocksToTime(blocks) {
	if (blocks < 144 * 7) return (blocks / 144).toFixed(1) + ' days';
	if (blocks < 144 * 30) return (blocks / 144 / 7).toFixed(1) + ' weeks';
	return (blocks / 144 / 30).toFixed(1) + ' months';
}

function retrieveContracts() {
	let active = 0;
	let passive = 0;
	let refreshed = 0;
	let disabled = 0;
	let a = document.getElementById('overview-contracts-active');
	let p = document.getElementById('overview-contracts-passive');
	let r = document.getElementById('overview-contracts-refreshed');
	let d = document.getElementById('overview-contracts-disabled');
	let t = document.getElementById('overview-contracts-total');
	a.innerHTML = active;
	p.innerHTML = passive;
	r.innerHTML = refreshed;
	d.innerHTML = disabled;
	t.innerHTML = active + passive + refreshed + disabled;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/contracts?&current=true&old=false', options)
		.then(response => response.json())
		.then(data => {
			if (!data) return;
			if (data.code) {
				console.log(data.message);
			} else {
				data.forEach((contract) => {
					switch (contract.status) {
					case 'active':
						active++;
						break;
					case 'passive':
						passive++;
						break;
					case 'refreshed':
						refreshed++;
						break;
					case 'disabled':
						disabled++;
						break;
					}
				});
				a.innerHTML = active;
				p.innerHTML = passive;
				r.innerHTML = refreshed;
				d.innerHTML = disabled;
				t.innerHTML = active + passive + refreshed + disabled;
			}
		})
		.catch(error => console.log(error));
}

function retrieveFiles() {
	let count = 0;
	let slabs = 0;
	let partial = 0;
	let pending = 0;
	let c = document.getElementById('overview-files-saved');
	let s = document.getElementById('overview-files-slabs');
	let p = document.getElementById('overview-files-partial');
	let b = document.getElementById('overview-files-pending');
	c.innerHTML = count;
	s.innerHTML = slabs;
	p.innerHTML = convertSize(partial);
	b.innerHTML = pending;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/files', options)
		.then(response => response.json())
		.then(data => {
			if (!data) return;
			if (data.code) {
				console.log(data.message);
			} else {
				data.forEach((item) => {
					if (item.size > 0 && !item.buffered) count++;
					slabs += item.slabs;
					partial += item.partialdata;
					if (item.buffered) pending++;
				});
				c.innerHTML = count;
				s.innerHTML = slabs;
				p.innerHTML = convertSize(partial);
				b.innerHTML = pending;
			}
		})
		.catch(error => console.log(error));
}

function changeCurrency(s) {
	averages.currency = s.value;
	document.getElementById('payment-currency').innerHTML = s.value;
	document.getElementById('contract-currency').innerHTML = s.value;
	document.getElementById('storage-currency').innerHTML = s.value;
	document.getElementById('upload-currency').innerHTML = s.value;
	document.getElementById('download-currency').innerHTML = s.value;
	retrieveAverages();
}

function changeInput() {
	document.getElementById('calculate-result').innerHTML = '';
	updateEstimation();
}

function setAverage(index) {
	switch (index) {
	case 0:
		document.getElementById('limits-contract').value =
			(averages.contractPrice * averages.rate).toPrecision(2);
		break;
	case 1:
		document.getElementById('limits-storage').value =
			(averages.storagePrice * averages.rate).toPrecision(2);
		break;
	case 2:
		document.getElementById('limits-upload').value =
			(averages.uploadBandwidthPrice * averages.rate).toPrecision(2);
		break;
	case 3:
		document.getElementById('limits-download').value =
			(averages.downloadBandwidthPrice * averages.rate).toPrecision(2);
		break;
	default:
	}
}

function updateEstimation() {
	if (processing) return;
	let payment = document.getElementById('select-payment');
	let currency = document.getElementById('select-currency').value;
	let duration = parseFloat(document.getElementById('select-duration').value);
	let storage = parseFloat(document.getElementById('select-storage').value);
	let upload = parseFloat(document.getElementById('select-upload').value);
	let download = parseFloat(document.getElementById('select-download').value);
	let hosts = parseInt(document.getElementById('select-hosts').value);
	let redundancy = parseFloat(document.getElementById('select-redundancy').value);
	if (isNaN(duration) || duration <= 0 ||
		isNaN(storage) || storage <= 0 ||
		isNaN(upload) || upload <= 0 ||
		isNaN(download) || download <= 0 ||
		isNaN(hosts) || hosts <= 0 ||
		isNaN(redundancy) || redundancy < 1) {
		document.getElementById('payment-calculate').disabled = true;
		return;
	}
	document.getElementById('payment-calculate').disabled = false;
	let p = averages.contractPrice * hosts;
	p += averages.storagePrice * storage * redundancy * duration * 30 / 7 / 1024;
	p += averages.uploadBandwidthPrice * upload * redundancy / 1024;
	p += averages.downloadBandwidthPrice * download / 1024;
	p += averages.sectorAccessPrice * download / 256; // for 4MiB sectors
	p += averages.baseRPCPrice * (hosts + redundancy * 10 + download / upload); // rather a guess
	// Siafund fee including the host's collateral
	p += 0.039 * (p + averages.collateral * redundancy * duration * 30 / 7 / 1024);
	p *= 2; // for renewing
	p *= 1.2; // any extra costs
	paymentEstimation = p;
}

function calculatePayment() {
	let result = document.getElementById('calculate-result');
	result.innerHTML = '';
	let currency = document.getElementById('select-currency').value;
	let duration = parseFloat(document.getElementById('select-duration').value);
	let storage = parseFloat(document.getElementById('select-storage').value);
	let upload = parseFloat(document.getElementById('select-upload').value);
	let download = parseFloat(document.getElementById('select-download').value);
	let hosts = parseInt(document.getElementById('select-hosts').value);
	let redundancy = parseFloat(document.getElementById('select-redundancy').value);
	let maxContractPrice = parseFloat(document.getElementById('limits-contract').value);
	let maxStoragePrice = parseFloat(document.getElementById('limits-storage').value);
	let maxUploadPrice = parseFloat(document.getElementById('limits-upload').value);
	let maxDownloadPrice = parseFloat(document.getElementById('limits-download').value);
	let data = {
		numhosts: hosts,
		duration: duration,
		storage: storage,
		upload: upload,
		download: download,
		redundancy: redundancy,
		maxcontractprice: isNaN(maxContractPrice) || maxContractPrice < 0 ? 0 : maxContractPrice,
		maxstorageprice: isNaN(maxStoragePrice) || maxStoragePrice < 0 ? 0 : maxStoragePrice,
		maxuploadprice: isNaN(maxUploadPrice) || maxUploadPrice < 0 ? 0 : maxUploadPrice,
		maxdownloadprice: isNaN(maxDownloadPrice) || maxDownloadPrice < 0 ? 0 : maxDownloadPrice,
		estimation: paymentEstimation,
		currency: currency
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	processing = true;
	document.getElementById('payment-calculate').disabled = true;
	document.getElementById('calculate-text').classList.add('disabled');
	document.getElementById('calculate-spinner').classList.remove('disabled');
	fetch(apiBaseURL + '/dashboard/hosts', options)
		.then(response => {
			processing = false;
			document.getElementById('payment-calculate').disabled = false;
			document.getElementById('calculate-text').classList.remove('disabled');
			document.getElementById('calculate-spinner').classList.add('disabled');
			return response.json();
		})
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				paymentAmount = data.estimation;
				paymentCurrency = data.currency;
				if (data.numhosts == hosts) {
					result.classList.remove('error');
					result.innerHTML = 'Calculation successful, to pay: ' + paymentAmount +
						' ' + paymentCurrency;
				} else {
					result.classList.add('error');
					result.innerHTML = 'Warning: only ' + data.numhosts +
						' hosts found';
				}
				document.getElementById('payment-actual').value = paymentAmount;
				document.getElementById('payment-currency').innerHTML = paymentCurrency;
				document.getElementById('payment-actual').focus();
				document.getElementById('amount-proceed').disabled = false;
			}
		})
		.catch(error => console.log(error));
}

function paymentChange(obj) {
	let v = parseFloat(obj.value);
	if (!isNaN(v) && v > 0) {
		document.getElementById('amount-proceed').disabled = false;
	} else {
		document.getElementById('amount-proceed').disabled = true;
	}
}

function toPayment() {
	let a = document.getElementById('payment-actual');
	paymentAmount = parseFloat(a.value);
	paymentCurrency = averages.currency;
	initialize(false);
	paying = true;
	document.getElementById('to-pay').innerHTML = paymentAmount.toFixed(2) + ' ' +
		paymentCurrency;
	document.getElementById('select').classList.add('disabled');
	document.getElementById('sc').classList.add('disabled');
	document.getElementById('payment').classList.remove('disabled');
}

function backToSelect() {
	paying = false;
	document.getElementById('payment').classList.add('disabled');
	document.getElementById('select').classList.remove('disabled');
	document.getElementById('sc').classList.remove('disabled');
}

function changePaymentsStep(s) {
	userData.paymentsStep = parseInt(s.value);
	paymentsFrom = 1;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	renderPayments();
}

function renderPayments() {
	let tbody = document.getElementById('history-table');
	tbody.innerHTML = '';
	if (payments.length == 0) {
		document.getElementById('history-non-empty').classList.add('disabled');
		document.getElementById('history-empty').classList.remove('disabled');
		document.getElementById('history-prev').disabled = true;
		document.getElementById('history-next').disabled = true;
		paymentsFrom = 1;
		return;
	}
	payments.forEach((row, i) => {
		if (i < paymentsFrom - 1) return;
		if (i >= paymentsFrom + userData.paymentsStep - 1) return;
		timestamp = new Date(row.timestamp * 1000);
		let tr = document.createElement('tr');
		tr.innerHTML = `<td>${i + 1}</td>`;
		tr.innerHTML += `<td>${timestamp.toLocaleString()}</td>`;
		tr.innerHTML += `<td>${row.amount.toFixed(2)}</td>`;
		tr.innerHTML += `<td>${row.currency}</td>`;
		tr.innerHTML += `<td>${row.amountsc.toFixed(2)} SC</td>`;
		if (row.confirmations > 0) {
			tr.innerHTML += `<td>${6-row.confirmations}/6 confirmations</td>`;
		} else {
			tr.innerHTML += '<td></td>';
		}
		tbody.appendChild(tr);
	});
	document.getElementById('history-empty').classList.add('disabled');
	document.getElementById('history-non-empty').classList.remove('disabled');
	document.getElementById('history-prev').disabled = paymentsFrom == 1;
	document.getElementById('history-next').disabled = payments.length < paymentsFrom + userData.paymentsStep;
}

function getPayments() {
	let loading = document.getElementById('history-loading');
	if (!refreshing) {
		document.getElementById('history-empty').classList.add('disabled');
		document.getElementById('history-non-empty').classList.add('disabled');
		loading.classList.remove('disabled');
	}
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/payments', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				payments = data;
				if (userData.sortByTimestamp == 'ascending') {
					payments = payments.sort((a, b) => a.timestamp - b.timestamp);
				} else {
					payments = payments.sort((a, b) => b.timestamp - a.timestamp);
				}
				renderPayments();
				loading.classList.add('disabled');
			}
		})
		.catch(error => console.log(error));
}

function paymentsPrev() {
	paymentsFrom = paymentsFrom - userData.paymentsStep;
	if (paymentsFrom < 1) paymentsFrom = 1;
	renderPayments();
}

function paymentsNext() {
	paymentsFrom = paymentsFrom + userData.paymentsStep;
	renderPayments();
}

function copyPK() {
	let b = document.getElementById('copy-button');
	let k = document.getElementById('reveal-key');
	let pos = k.value.indexOf(':');
	k.select();
	k.setSelectionRange(pos + 1, 99);
	navigator.clipboard.writeText(k.value.slice(pos + 1));
	b.innerText = 'Copied!';
	window.setTimeout(function() {
		b.innerText = 'Copy';
	}, 1000);
}

function copyAddress() {
	let b = document.getElementById('copy-addr');
	let a = document.getElementById('reveal-addr');
	a.select();
	a.setSelectionRange(0, 99);
	navigator.clipboard.writeText(a.value);
	b.innerText = 'Copied!';
	window.setTimeout(function() {
		b.innerText = 'Copy';
	}, 1000);
}

function revealSeed() {
	let b = document.getElementById('reveal-button');
	let t = document.getElementById('reveal-text');
	if (b.innerText == 'Copy') {
		b.disabled = true;
		t.select();
		t.setSelectionRange(0, 99);
		navigator.clipboard.writeText(t.value);
		b.innerText = 'Copied!';
		window.setTimeout(function() {
			t.value = '';
			b.innerText = 'Show';
			b.disabled = false;
		}, 1000);
		return;
	}
	b.disabled = true;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/seed', options)
		.then(response => {
			if (response.status == 204) {
				h = response.headers.get('Renter-Seed');
				t.value = h;
				b.innerText = 'Copy';
				b.disabled = false;
				return '';
			} else return response.json();
		})
		.then(data => console.log(data))
		.catch(error => console.log(error));
}

function retrieveKey() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/key', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data.message);
			} else {
				document.getElementById('reveal-key').value = data.key;
				document.getElementById('reveal-addr').value = window.location.hostname + ':' + data.satport;
				document.getElementById('reveal-mux').value = data.muxport;
			}
		})
		.catch(error => console.log(error));
}

function retrieveBlockHeight() {
	let bh = document.getElementById('block-height');
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/blockheight', options)
		.then(response => response.json())
		.then(data => {
			if (data.height) {
				bh.innerHTML = data.height;
			}
		})
		.catch(error => console.log(error));
}

function changeContractsStep(s) {
	userData.contractsStep = parseInt(s.value);
	contractsFrom = 1;
	expandedContract = -1;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	renderContracts();
}

var contractsClick;

function renderContracts() {
	let tbody = document.getElementById('contracts-table');
	tbody.innerHTML = '';
	if (contracts.length == 0) {
		document.getElementById('contracts-non-empty').classList.add('disabled');
		document.getElementById('contracts-empty').classList.remove('disabled');
		document.getElementById('contracts-prev').disabled = true;
		document.getElementById('contracts-next').disabled = true;
		contractsFrom = 1;
		return;
	}
	contracts.forEach((row, i) => {
		if (i < contractsFrom - 1) return;
		if (i >= contractsFrom + userData.contractsStep - 1) return;
		let tr = document.createElement('tr');
		tr.innerHTML = `<td>${i + 1}</td>`;
		tr.innerHTML += `<td class="cell-overflow">${row.id.slice(row.id.indexOf(':') + 1)}</td>`;
		tr.innerHTML += `<td>${row.startheight}</td>`;
		tr.innerHTML += `<td>${row.endheight}</td>`;
		tr.innerHTML += `<td class="cell-overflow">${row.netaddress}</td>`;
		tr.innerHTML += `<td>${row.size}</td>`;
		tr.innerHTML += `<td>${row.totalcost}</td>`;
		tr.innerHTML += `<td>${row.status}</td>`;
		tr.index = i;
		tr.addEventListener("mousedown", (e) => {
			contractsClick = {x: e.clientX, y: e.clientY};
		});
		tr.addEventListener("mouseup", (e) => {
			if (contractsClick.x == e.clientX && contractsClick.y == e.clientY) expandContract(e);
		});
		tbody.appendChild(tr);
	});
	document.getElementById('contracts-empty').classList.add('disabled');
	document.getElementById('contracts-non-empty').classList.remove('disabled');
	document.getElementById('contracts-prev').disabled = contractsFrom == 1;
	document.getElementById('contracts-next').disabled = contracts.length < contractsFrom + userData.contractsStep;
}

function expandContract(e) {
	let tbody = document.getElementById('contracts-table');
	let index = e.currentTarget.index
	if (expandedContract == index) {
		expandedContract = -1;
		tbody.removeChild(tbody.children[index - contractsFrom + 2]);
		return;
	}
	if (expandedContract >= 0) {
		tbody.removeChild(tbody.children[expandedContract - contractsFrom + 2]);
	}
	expandedContract = index;
	tr = document.createElement('tr');
	tr.classList.add('contracts-expand');
	tr.innerHTML = '<td></td>';
	tr.innerHTML += '<td>Contract ID:<br>Host:<br>Host Public Key:<br>Host Version:<br>' +
		'Storage Spending:<br>Upload Spending:<br>Download Spending:<br>' +
		'Fund Account Spending:<br>Fees:<br>Remaining Funds:<br>Remaining Collateral:</td>';
	tr.innerHTML += '<td colspan="6">' + contracts[index].id + '<br>' +
		contracts[index].netaddress + '<br>' + contracts[index].hostpublickey + '<br>' +
		contracts[index].hostversion + '<br>' + contracts[index].storagespending + '<br>' +
		contracts[index].uploadspending + '<br>' + contracts[index].downloadspending + '<br>' +
		contracts[index].fundaccountspending + '<br>' + contracts[index].fees + '<br>' +
		contracts[index].renterfunds + '<br>' + contracts[index].remainingcollateral + '</td>';
	tbody.children[index - contractsFrom + 1].insertAdjacentElement("afterend", tr);
}

function getContracts() {
	let current = userData.currentContracts;
	let old = userData.oldContracts;
	let loading = document.getElementById('contracts-loading');
	if (!refreshing) {
		document.getElementById('contracts-empty').classList.add('disabled');
		document.getElementById('contracts-non-empty').classList.add('disabled');
		loading.classList.remove('disabled');
	}
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/contracts?&current=' + current + '&old=' + old, options)
		.then(response => response.json())
		.then(data => {
			if (!data) {
				contracts = [];
				renderContracts();
				loading.classList.add('disabled');
				return;
			}
			if (data.code) {
				console.log(data);
			} else {
				contracts = data;
				if (userData.sortByStart == 'ascending') {
					contracts = contracts.sort((a, b) => a.startheight - b.startheight);
				}
				if (userData.sortByStart == 'descending') {
					contracts = contracts.sort((a, b) => b.startheight - a.startheight);
				}
				if (userData.sortByEnd == 'ascending') {
					contracts = contracts.sort((a, b) => a.endheight - b.endheight);
				}
				if (userData.sortByEnd == 'decending') {
					contracts = contracts.sort((a, b) => b.endheight - a.endheight);
				}
				renderContracts();
				loading.classList.add('disabled');
			}
		})
		.catch(error => console.log(error));
}

function contractsPrev() {
	contractsFrom = contractsFrom - userData.contractsStep;
	if (contractsFrom < 1) contractsFrom = 1;
	expandedContract = -1;
	renderContracts();
}

function contractsNext() {
	contractsFrom = contractsFrom + userData.contractsStep;
	expandedContract = -1;
	renderContracts();
}

function contractsChanged() {
	contractsFrom = 1;
	expandedContract = -1;
	userData.currentContracts = document.getElementById('contracts-current').checked;
	userData.oldContracts = document.getElementById('contracts-old').checked;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	getContracts();
}

function sortByContractStart() {
	switch (userData.sortByStart) {
	case 'inactive':
		userData.sortByStart = 'ascending';
		document.getElementById('contracts-start-asc').classList.add('active');
		userData.sortByEnd = 'inactive';
		document.getElementById('contracts-end-desc').classList.remove('active');
		document.getElementById('contracts-end-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.startheight - b.startheight);
		break;
	case 'ascending':
		userData.sortByStart = 'descending';
		document.getElementById('contracts-start-desc').classList.add('active');
		document.getElementById('contracts-start-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => b.startheight - a.startheight);
		break;
	case 'descending':
		userData.sortByStart = 'ascending';
		document.getElementById('contracts-start-asc').classList.add('active');
		document.getElementById('contracts-start-desc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.startheight - b.startheight);
		break;
	default:
	}
	expandedContract = -1;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	renderContracts();
}

function sortByContractEnd() {
	switch (userData.sortByEnd) {
	case 'inactive':
		userData.sortByEnd = 'ascending';
		document.getElementById('contracts-end-asc').classList.add('active');
		userData.sortByStart = 'inactive';
		document.getElementById('contracts-start-desc').classList.remove('active');
		document.getElementById('contracts-start-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.endheight - b.endheight);
		break;
	case 'ascending':
		userData.sortByEnd = 'descending';
		document.getElementById('contracts-end-desc').classList.add('active');
		document.getElementById('contracts-end-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => b.endheight - a.endheight);
		break;
	case 'descending':
		userData.sortByEnd = 'ascending';
		document.getElementById('contracts-end-asc').classList.add('active');
		document.getElementById('contracts-end-desc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.endheight - b.endheight);
		break;
	default:
	}
	expandedContract = -1;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	renderContracts();
}

function getSpendings() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/spendings?currency=' + averages.currency, options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				const date = new Date();
				let cm = date.getMonth();
				let cy = date.getFullYear();
				let pm = cm > 0 ? cm - 1 : 11;
				let py = cm > 0 ? cy : cy - 1;
				let c = document.getElementById('spendings-current');
				let p = document.getElementById('spendings-prev');
				let clp = document.getElementById('spendings-current-locked-primary');
				let cls = document.getElementById('spendings-current-locked-secondary');
				let cup = document.getElementById('spendings-current-used-primary');
				let cus = document.getElementById('spendings-current-used-secondary');
				let cop = document.getElementById('spendings-current-overhead-primary');
				let cos = document.getElementById('spendings-current-overhead-secondary');
				let plp = document.getElementById('spendings-prev-locked-primary');
				let pls = document.getElementById('spendings-prev-locked-secondary');
				let pup = document.getElementById('spendings-prev-used-primary');
				let pus = document.getElementById('spendings-prev-used-secondary');
				let pop = document.getElementById('spendings-prev-overhead-primary');
				let pos = document.getElementById('spendings-prev-overhead-secondary');
				let cf = document.getElementById('spendings-current-formed');
				let cr = document.getElementById('spendings-current-renewed');
				let cs = document.getElementById('spendings-current-saved');
				let crr = document.getElementById('spendings-current-retrieved');
				let cmm = document.getElementById('spendings-current-migrated');
				let pf = document.getElementById('spendings-prev-formed');
				let pr = document.getElementById('spendings-prev-renewed');
				let ps = document.getElementById('spendings-prev-saved');
				let prr = document.getElementById('spendings-prev-retrieved');
				let pmm = document.getElementById('spendings-prev-migrated');
				c.innerHTML = month[cm] + ' ' + cy;
				p.innerHTML = month[pm] + ' ' + py;
				clp.innerHTML = (data[0].locked * data[0].scrate).toFixed(2) +
					' ' + averages.currency;
				cls.innerHTML = data[0].locked.toFixed(2) + ' SC';
				cup.innerHTML = (data[0].used * data[0].scrate).toFixed(2) +
					' ' + averages.currency;
				cus.innerHTML = data[0].used.toFixed(2) + ' SC';
				cop.innerHTML = (data[0].overhead * data[0].scrate).toFixed(2) +
					' ' + averages.currency;
				cos.innerHTML = data[0].overhead.toFixed(2) + ' SC';
				cf.innerHTML = data[0].formed;
				cr.innerHTML = data[0].renewed;
				cs.innerHTML = data[0].slabssaved;
				crr.innerHTML = data[0].slabsretrieved;
				cmm.innerHTML = data[0].slabsmigrated;
				plp.innerHTML = (data[1].locked * data[1].scrate).toFixed(2) +
					' ' + averages.currency;
				pls.innerHTML = data[1].locked.toFixed(2) + ' SC';
				pup.innerHTML = (data[1].used * data[1].scrate).toFixed(2) +
					' ' + averages.currency;
				pus.innerHTML = data[1].used.toFixed(2) + ' SC';
				pop.innerHTML = (data[1].overhead * data[1].scrate).toFixed(2) +
					' ' + averages.currency;
				pos.innerHTML = data[1].overhead.toFixed(2) + ' SC';
				pf.innerHTML = data[1].formed;
				pr.innerHTML = data[1].renewed;
				ps.innerHTML = data[1].slabssaved;
				prr.innerHTML = data[1].slabsretrieved;
				pmm.innerHTML = data[1].slabsmigrated;
			}
		})
		.catch(error => console.log(error));
}

function getSettings() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/settings', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				let ar = document.getElementById('settings-autorenew');
				let md = document.getElementById('settings-metadata');
				let fr = document.getElementById('settings-autorepair');
				let pu = document.getElementById('settings-proxy');
				ar.checked = data.autorenew;
				ar.disabled = !data.autorenew;
				md.checked = data.backupmetadata;
				md.disabled = !data.backupmetadata;
				fr.checked = data.autorepair;
				fr.disabled = !data.autorepair;
				pu.checked = data.proxyuploads;
				pu.disabled = !data.proxyuploads;
			}
		})
		.catch(error => console.log(error));
}

function convertSize(size) {
	if (size < 1024) {
		return '' + size + ' B';
	}
	const sizes = ['KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
	let i = Math.floor(Math.log10(size) / 3);
	let s = '' + size / Math.pow(10, 3 * i);
	return s.slice(0, s.indexOf('.') + i + 1) + ' ' + sizes[i - 1];
}

function renderFiles() {
	if (files.length == 0) {
		document.getElementById('files-non-empty').classList.add('disabled');
		document.getElementById('files-empty').classList.remove('disabled');
		return;
	}
	if (!userData.encryptionKey) {
		let encrypted = false;
		files.forEach((file) => {
			if (file.parts) {
				encrypted = true;
			}
		});
		if (encrypted) {
			document.getElementById('files-results').classList.add('disabled');
			document.getElementById('files-nokey').classList.remove('disabled');
			document.getElementById('files-non-empty').classList.remove('disabled');
			return;
		}
	}
	let container = document.getElementById('files-container');
	container.innerHTML = '';
	let filepath = document.getElementById('files-path');
	if (currentBucket == '') {
		buckets.forEach((bucket) => {
			let b = document.createElement('div');
			b.classList.add('files-item');
			b.innerHTML = `<img src="assets/bucket.png"><div>${bucket.name}</div>`;
			b.addEventListener("click", () => {expandBucket(bucket)});
			container.appendChild(b);
		});
	} else {
		let items = getItems(currentPath);
		let i = document.createElement('div');
		i.classList.add('files-item');
		i.innerHTML = '<img src="assets/folder.png"><div>..</div>';
		if (currentPath == '') {
			i.addEventListener("click", () => {collapseBucket()});
		} else {
			i.addEventListener("click", () => {collapseDir()});
		}
		container.appendChild(i);
		items.forEach((item) => {
			let i = document.createElement('div');
			i.classList.add('files-item');
			if (item.files) {
				if (selectedFiles.findIndex(it => it.path == item.path) >= 0) {
					i.classList.add('files-item-selected');
				}
				i.innerHTML = `<img src="assets/folder.png"><div>${item.name}</div>`;
				i.appendChild(createDirHint(item));
				i.addEventListener("click", (e) => {
					if (e.shiftKey) {
						let index = selectedFiles.findIndex(it => it.path == item.path);
						if (index >= 0) {
							iterateOverAll(item, (file) => {
								let j = selectedFiles.findIndex(it => it.path == file.path);
								if (j >= 0) selectedFiles.splice(j, 1);
							});
							index = selectedFiles.findIndex(it => it.path == item.path);
							selectedFiles.splice(index, 1);
							i.classList.remove('files-item-selected');
							let count = countSelectedFiles();
							filepath.innerHTML = `Current path: ${getCurrentPath()}`;
							if (count > 0) {
								filepath.innerHTML += ` (selected ${count} files)`;
							} else {
								document.getElementById('files-download').disabled = true;
							}
							if (selectedFiles.length == 0) {
								document.getElementById('files-delete').disabled = true;
							}
						} else {
							selectedFiles.push({
								item: item.item,
								bucket: item.bucket,
								path: item.path
							});
							iterateOverAll(item, (file) => {
								selectedFiles.push({
									item: file.item,
									bucket: file.bucket,
									path: file.path
								});
							});
							i.classList.add('files-item-selected');
							let count = countSelectedFiles();
							filepath.innerHTML = `Current path: ${getCurrentPath()}`;
							if (count > 0) {
								filepath.innerHTML += ` (selected ${count} files)`;
								document.getElementById('files-download').disabled = false;
							}
							document.getElementById('files-delete').disabled = false;

						}
					} else expandDir(item.path);
				});
			} else {
				if (item.item.buffered) i.classList.add('files-item-temp');
				if (selectedFiles.findIndex(it => it.path == item.path) >= 0) {
					i.classList.add('files-item-selected');
				}
				i.innerHTML = `<img src="assets/file.png"><div>${item.name}</div>`;
				i.appendChild(createFileHint(item));
				i.addEventListener("click", () => {
					let index = selectedFiles.findIndex(it => it.path == item.path);
					if (index >= 0) {
						selectedFiles.splice(index, 1);
						i.classList.remove('files-item-selected');
						let count = countSelectedFiles();
						filepath.innerHTML = `Current path: ${getCurrentPath()}`;
						if (count > 0) {
							filepath.innerHTML += ` (selected ${count} files)`;
						} else {
							document.getElementById('files-download').disabled = true;
						}
						if (selectedFiles.length == 0) {
							document.getElementById('files-delete').disabled = true;
						}
					} else {
						selectedFiles.push({
							item: item.item,
							bucket: item.bucket,
							path: item.path
						});
						i.classList.add('files-item-selected');
						let count = countSelectedFiles();
						filepath.innerHTML = `Current path: ${getCurrentPath()}`;
						if (count > 0) filepath.innerHTML += ` (selected ${count} files)`;
						document.getElementById('files-delete').disabled = false;
						document.getElementById('files-download').disabled = false;
					}
				});
			}
			container.appendChild(i);
			let h = i.lastElementChild;
			if (i.offsetTop - container.offsetTop + i.clientHeight - h.clientHeight >= getRemSize() * 3) {
				i.lastElementChild.style.bottom = '3rem';
			} else {
				i.lastElementChild.style.top = '-1rem';
			}
			if (container.offsetLeft + container.clientWidth - i.offsetLeft - h.clientWidth >= getRemSize() * 2) {
				i.lastElementChild.style.left = '2rem';
			} else {
				i.lastElementChild.style.right = '2rem';
			}
		});
	}
	document.getElementById('files-nokey').classList.add('disabled');
	document.getElementById('files-results').classList.remove('disabled');
	document.getElementById('files-empty').classList.add('disabled');
	document.getElementById('files-non-empty').classList.remove('disabled');
	if (downloads.length == 0) {
		document.getElementById('files-downloads').classList.add('disabled');
		return;
	}
	let table = document.getElementById('files-table');
	table.innerHTML = '';
	downloads.forEach((download, index) => {
		let tr = document.createElement('tr');
		tr.innerHTML += `<td>${index + 1}</td>`;
		tr.innerHTML += `<td><span id=${'cancel-' + encodeURI(download.bucket) + encodeURI(download.path)} class="cancel">&#9421;</span></td>`;
		tr.innerHTML += `<td>${download.bucket}</td>`;
		tr.innerHTML += `<td>${download.path}</td>`;
		tr.innerHTML += `<td>${convertSize(download.item.size)}</td>`;
		let loaded = (download.loaded == 0 || downloads.loaded == download.item.size) ? '<span class="loading"></span>' :
			(download.loaded / download.item.size * 100).toFixed(0) + '%';
		tr.innerHTML += `<td id=${'progress-' + encodeURI(download.bucket) + encodeURI(download.path)}>${loaded}</td>`;
		table.appendChild(tr);
		document.getElementById('cancel-' + encodeURI(download.bucket) + encodeURI(download.path)).addEventListener('click', () => {
			download.controller.abort();
		});
	});
	document.getElementById('files-downloads').classList.remove('disabled');

}

function getItems(path) {
	let bucket = buckets.find(b => b.name == currentBucket);
	if (path == '') {
		return bucket ? bucket.files : [];
	}
	let dir = findItem(bucket, item => item.path == path);
	return dir ? dir.files : [];
}

function expandBucket(bucket) {
	cancelSelection();
	currentBucket = buckets.find(b => b.name == bucket.name).name;
	document.getElementById('files-path').innerHTML = `Current path: ${getCurrentPath()}`;
	renderFiles();
}

function collapseBucket() {
	cancelSelection();
	currentBucket = '';
	currentPath = '';
	document.getElementById('files-path').innerHTML = `Current path: ..`;
	renderFiles();
}

function expandDir(path) {
	cancelSelection();
	currentPath = path;
	document.getElementById('files-path').innerHTML = `Current path: ${getCurrentPath()}`;
	renderFiles();
}

function collapseDir() {
	cancelSelection();
	currentPath = currentPath.slice(0, currentPath.length - 1);
	let i = currentPath.lastIndexOf('/');
	if (i < 0) {
		currentPath = '';
	} else {
		currentPath = currentPath.slice(0, i) + '/';
	}
	document.getElementById('files-path').innerHTML = `Current path: ${getCurrentPath()}`;
	renderFiles();
}

function getCurrentPath() {
	if (currentBucket == '') return '..';
	path = currentBucket + `>`;
	return '<code>' + path + currentPath + '</code>';
}

function createFileHint(item) {
	let h = document.createElement('div');
	h.classList.add('files-hint');
	h.innerHTML = `<div><label>Size:</label><span>${convertSize(item.item.size)}</span></div>`;
	h.innerHTML += `<div><label>Slabs:</label><span>${item.item.buffered ? 'N/A' : item.item.slabs}</span></div>`;
	h.innerHTML += `<div><label>Partial Slab:</label><span>${convertSize(item.item.partialdata)}</span></div>`;
	h.innerHTML += `<div><label>${item.item.buffered ? 'Submitted' : 'Uploaded'}:</label><span>${new Date(item.item.uploaded * 1000).toLocaleString()}</span></div>`;
	return h;
}

function createDirHint(item) {
	let count = 0;
	iterateOverFiles(item, () => {count++});
	let h = document.createElement('div');
	h.classList.add('files-hint');
	h.innerHTML = `<div><label>Files:</label><span>${count}</span></div>`;
	h.innerHTML += `<div><label>Created:</label><span>${new Date(item.item.uploaded * 1000).toLocaleString()}</span></div>`;
	return h;
}

function iterateOverFiles(item, func) {
	item.files.forEach((file) => {
		if (file.files) {
			iterateOverFiles(file, func);
		} else func(file);
	});
}

function iterateOverAll(item, func) {
	item.files.forEach((file) => {
		func(file);
		if (file.files) {
			iterateOverFiles(file, func);
		}
	});
}

function findItem(item, func) {
	let res = item.files.find(i => func(i));
	if (res) return res;
	for (let i = 0; i < item.files.length; i++) {
		if (item.files[i].files) return findItem(item.files[i], func);
	}
	return null;
}

function countSelectedFiles() {
	let count = 0;
	selectedFiles.forEach((item) => {
		if (item.path[item.path.length - 1] != '/') count++;
	});
	return count;
}

function cancelSelection() {
	selectedFiles = [];
	document.getElementById('files-delete').disabled = true;
	document.getElementById('files-download').disabled = true;
	document.getElementById('files-path').innerHTML = `Current path: ${getCurrentPath()}`;
}

function deselect(obj, e) {
	if (e.target == obj) {
		cancelSelection();
		renderFiles();
	}
}

function downloadFiles() {
	selectedFiles.forEach((file) => {
		if (file.path[file.path.length - 1] == '/') return;
		if (file.item.buffered) return;
		if (downloads.find(item => item.bucket == file.bucket && item.path == file.path)) return;
		const controller = new AbortController();
		downloads.push({
			item: file.item,
			bucket: file.bucket,
			path: file.path,
			loaded: 0,
			controller: controller,
		});
		downloadFile(downloads[downloads.length - 1]);
	});
}

function downloadFile(download) {
	renderFiles();
	let options = {
		method: 'GET',
		signal: download.controller.signal,
	}
	document.getElementById('progress-' + encodeURI(download.bucket) + encodeURI(download.path)).innerHTML = '<span class="loading"></span>';
	document.getElementById('cancel-' + encodeURI(download.bucket) + encodeURI(download.path)).addEventListener('click', () => {
		download.controller.abort();
	});
	fetch(apiBaseURL + '/dashboard/file?bucket=' + download.item.bucket + '&path=' + download.item.path, options)
		.then(async (response) => {
			if (response.status == 200) {
				const reader = response.body.getReader();
				let loaded = 0;
				let percent = 0;
				let chunks = [];
				let cc;
				if (download.item.parts) {
					cc = new ChaCha20(Uint8Array.from(userData.encryptionKey), download.item.parts);
				}
				while(true) {
					const {done, value} = await reader.read();
					if (done) {
						break;
					}
					if (download.item.parts) {
						cc.decrypt(value);
					}
					chunks.push(value);
					loaded += value.length;
					download.loaded = loaded;
					if (download.item.size > 0) {
						percent = loaded / download.item.size * 100;
					}
					document.getElementById('progress-' + encodeURI(download.bucket) + encodeURI(download.path)).innerHTML = percent.toFixed(0) + '%';
				}
				return new Blob(chunks);
			} else {
				return response.json();
			}
		})
		.then(blob => {
			if (blob.code) {
				console.log(blob.message);
				return;
			}
			document.getElementById('progress-' + encodeURI(download.bucket) + encodeURI(download.path)).innerHTML = '<span class="loading"></span>';
			let url = window.URL.createObjectURL(blob);
			let a = document.createElement("a");
			a.href = url;
			a.setAttribute("download", download.path.slice(download.path.lastIndexOf('/') + 1));
			a.click();
			window.URL.revokeObjectURL(url);
		})
		.catch(error => console.log(error))
		.finally(() => {
			downloads.splice(downloads.findIndex(item => item.bucket == download.bucket && item.path == download.path), 1);
			renderFiles();
		});
}

function getFiles() {
	let loading = document.getElementById('files-loading');
	if (!refreshing) {
		document.getElementById('files-empty').classList.add('disabled');
		document.getElementById('files-non-empty').classList.add('disabled');
		loading.classList.remove('disabled');
	}
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/files', options)
		.then(response => response.json())
		.then(data => {
			if (!data) data = [];
			if (data.code) {
				console.log(data);
			} else {
				files = data;
				buckets = [];
				files.forEach((item) => {
					addItem(item);
				});
				if (currentPath == '') {
					let items = getItems(currentPath);
					if (items.length == 0) {
						collapseBucket();
					}
				}
				renderFiles();
				loading.classList.add('disabled');
				let slabs = 0;
				let partial = 0;
				let pending = 0;
				data.forEach(file => {
					slabs += file.slabs;
					partial += file.partialdata;
					pending += file.buffered ? 1 : 0;
				});
				document.getElementById('files-slabs').innerHTML = slabs;
				document.getElementById('files-partial').innerHTML = convertSize(partial);
				document.getElementById('files-pending').innerHTML = pending;
			}
		})
		.catch(error => console.log(error));
}

function addItem(item) {
	let bn = decryptString(item.bucket, item.parts);
	let fp = decryptString(item.path, item.parts).slice(1);
	let bi = buckets.findIndex(b => b.name == bn);
	if (bi < 0) {
		buckets.push({ name: bn, files: [] });
		bi = buckets.length - 1;
	}
	let path = fp;
	let bf = buckets[bi].files;
	let name = '';
	while (path.length > 0 && path.indexOf('/') >=0) {
		let name = path.slice(0, path.indexOf('/'));
		let fi = bf.findIndex(f => f.name == name);
		if (fi < 0) {
			bf.push({ name: name, files: [] })
			fi = bf.length - 1;
		}
		path = path.slice(path.indexOf('/') + 1);
		if (path == '') {
			bf[fi].item = item;
			bf[fi].bucket = bn;
			bf[fi].path = fp;
			return;
		}
		bf = bf[fi].files;
	}
	bf.push({
		name: path,
		item: item,
		bucket: bn,
		path: fp
	})
}

function deleteFiles() {
	let data = {
		files: []
	}
	selectedFiles.forEach((item) => {
		let file = downloads.find(it => it.bucket == item.bucket && it.path == item.path);
		if (file) {
			file.controller.abort();
		}
		data.files.push({
			bucket: item.item.bucket,
			path: item.item.path,
			buffered: item.item.buffered
		});
	});
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/dashboard/files', options)
		.then(response => {
			if (response.status == 204) {
				cancelSelection();
				getFiles();
			} else {
				return response.json();
			}
		})
		.then(data => {
			if (data) {
				console.log(data);
			}
		})
		.catch(error => console.log(error));
}

function getVersion() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/version', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				document.getElementById('version').innerHTML = data.version;
			}
		})
		.catch(error => console.log(error));
}

function getCurrencies() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/stripe/currencies', options)
		.then(response => response.json())
		.then(data => {
			currencies = data.currencies;
			let sc = document.getElementById('select-currency');
			currencies.forEach((cur) => {
				let op = document.createElement('option');
				op.value = cur.name;
				op.innerHTML = cur.name;
				sc.appendChild(op);
			});
		})
		.catch(error => console.log(error));
}

function sortByPaymentTime() {
	switch (userData.sortByTimestamp) {
	case 'ascending':
		userData.sortByTimestamp = 'descending';
		document.getElementById('history-timestamp-desc').classList.add('active');
		document.getElementById('history-timestamp-asc').classList.remove('active');
		payments = payments.sort((a, b) => b.timestamp - a.timestamp);
		break;
	case 'descending':
		userData.sortByTimestamp = 'ascending';
		document.getElementById('history-timestamp-asc').classList.add('active');
		document.getElementById('history-timestamp-desc').classList.remove('active');
		payments = payments.sort((a, b) => a.timestamp - b.timestamp);
		break;
	default:
	}
	window.localStorage.setItem('userData', JSON.stringify(userData));
	renderPayments();
}

function settingsChange(e) {
	let rn = document.getElementById('settings-autorenew');
	let md = document.getElementById('settings-metadata');
	let rp = document.getElementById('settings-autorepair');
	let pr = document.getElementById('settings-proxy');
	let data = {
		autorenew: rn.checked,
		backupmetadata: md.checked,
		autorepair: (rn.checked && md.checked) ? rp.checked : false,
		proxyuploads: md.checked ? pr.checked : false
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/dashboard/settings', options)
		.then(response => {
			if (response.status == 204) {
				switch (e.id) {
				case 'settings-autorenew':
					if (!e.checked) {
						rp.checked = false;
						rp.disabled = true;
						e.disabled = true;
					}
					break;
				case 'settings-metadata':
					if (!e.checked) {
						rp.checked = false;
						rp.disabled = true;
						pr.checked = false;
						pr.disabled = true;
						e.disabled = true;
					}
					break;
				case 'settings-autorepair':
					if (!e.checked) {
						e.disabled = true;
					}
					break;
				case 'settings-proxy':
					if (!e.checked) {
						e.disabled = true;
					}
					break;
				default:
				}
				return '';
			} else {
				e.checked = !e.checked;
				return response.json();
			}
		})
		.then(data => {
			if (data) {
				console.log(data);
			}
		})
		.catch(error => console.log(error));
}

function showAddress() {
	let g = document.getElementById('sc-generate');
	let a = document.getElementById('sc-address');
	if (g.innerText == 'Copy') {
		g.disabled = true;
		a.select();
		a.setSelectionRange(0, 99);
		navigator.clipboard.writeText(a.value);
		g.innerText = 'Copied!';
		window.setTimeout(function() {
			g.innerText = 'Copy';
			g.disabled = false;
		}, 1000);
		return;
	}
	g.disabled = true;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/address', options)
		.then(response => response.json())
		.then(data => {
			if (data.address) {
				a.value = data.address;
				g.innerText = 'Copy';
				g.disabled = false;
			}
		})
		.catch(error => console.log(error));
}

function changePaymentPlan() {
	let sc = document.getElementById('select-change');
	sc.disabled = true;
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/plan', options)
		.then(response => {
			if (response.status == 204) {
				sc.innerHTML = 'Success!';
				window.setTimeout(function() {
					sc.disabled = false;
					retrieveBalance();
				}, 1000);
			} else {
				return response.json()
			}
		})
		.then(data => {
			if (data && data.code) {
				console.log(data.message);
				return;
			}
			if (data && data.dpm == false) {
				paymentCurrency = averages.currency;
				paymentAmount = currencies.find(cur => cur.name == paymentCurrency).minimum;
				initialize(true);
				paying = true;
				document.getElementById('to-pay').innerHTML = paymentAmount.toFixed(2) + ' ' +
					paymentCurrency;
				document.getElementById('select').classList.add('disabled');
				sc.classList.add('disabled');
				document.getElementById('payment').classList.remove('disabled');
			}
		})
		.catch(error => console.log(error));
}

function changeEncryptionKey() {
	let key = document.getElementById('files-key').value.toLowerCase();
	if (key == keyValue) return;
	keyValue = key;
	if (key.length != 64 || /[^a-f0-9]/.test(key)) {
		userData.encryptionKey = null;
		window.localStorage.setItem('userData', JSON.stringify(userData));
		renderFiles();
		return;
	}
	let bytes = [];
	while (key.length > 0) {
		bytes.push(parseInt(key.slice(0, 2), 16));
		key = key.slice(2);
	}
	userData.encryptionKey = bytes;
	window.localStorage.setItem('userData', JSON.stringify(userData));
	getFiles();
}

function decryptString(base64, parts) {
	base64 = base64.replace(/-/g, '+').replace(/_/g, '/');
	let ciphertext = Uint8Array.from(atob(base64), (m) => m.codePointAt(0));
	if (parts && userData.encryptionKey) {
		let cc = new ChaCha20(Uint8Array.from(userData.encryptionKey));
		cc.decrypt(ciphertext);
	}
	return new TextDecoder('utf-8').decode(ciphertext);
}

class ChaCha20 {
	constructor(key, parts) {
		let get32 = (data, i) => data[i++] ^ (data[i++] << 8) ^ (data[i++] << 16) ^ (data[i] << 24);
		this.state = [
			0x61707865, 0x3320646e, 0x79622d32, 0x6b206574,
			get32(key, 0), get32(key, 4), get32(key, 8), get32(key, 12),
			get32(key, 16), get32(key, 20), get32(key, 24), get32(key, 28),
			0, 0, 0, 0
		];
		let temp = Array.from(this.state);
		for (let i = 0; i < 10; i++) {
			this.quarterRound(temp, 0, 4, 8, 12);
			this.quarterRound(temp, 1, 5, 9, 13);
			this.quarterRound(temp, 2, 6, 10, 14);
			this.quarterRound(temp, 3, 7, 11, 15);
			this.quarterRound(temp, 0, 5, 10, 15);
			this.quarterRound(temp, 1, 6, 11, 12);
			this.quarterRound(temp, 2, 7, 8, 13);
			this.quarterRound(temp, 3, 4, 9, 14);
		}
		this.state[4] = temp[0];
		this.state[5] = temp[1];
		this.state[6] = temp[2];
		this.state[7] = temp[3];
		this.state[8] = temp[12];
		this.state[9] = temp[13];
		this.state[10] = temp[14];
		this.state[11] = temp[15];
		this.buf = [
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
		];
		this.counter = 0;
		this.bytesRead = 0;
		this.parts = parts;
		this.part = 0;
	}
	quarterRound(data, a, b, c, d) {
		let rotl = (data, i) => (data << i) | (data >>> (32 - i));
		data[d] = rotl(data[d] ^ (data[a] += data[b]), 16);
		data[b] = rotl(data[b] ^ (data[c] += data[d]), 12);
		data[d] = rotl(data[d] ^ (data[a] += data[b]), 8);
		data[b] = rotl(data[b] ^ (data[c] += data[d]), 7);
		data[a] >>>= 0;
		data[b] >>>= 0;
		data[c] >>>= 0;
		data[d] >>>= 0;
	}
	decrypt(ciphertext) {
		for (let i = 0; i < ciphertext.length; i++) {
			if (this.counter == 0 || this.counter == 64) {
				let b = 0;
				let temp = Array.from(this.state);
				for (let j = 0; j < 10; j++) {
					this.quarterRound(temp, 0, 4, 8, 12);
					this.quarterRound(temp, 1, 5, 9, 13);
					this.quarterRound(temp, 2, 6, 10, 14);
					this.quarterRound(temp, 3, 7, 11, 15);
					this.quarterRound(temp, 0, 5, 10, 15);
					this.quarterRound(temp, 1, 6, 11, 12);
					this.quarterRound(temp, 2, 7, 8, 13);
					this.quarterRound(temp, 3, 4, 9, 14);
				}
				for (let j = 0; j < 16; j++) {
					temp[j] += this.state[j];
					this.buf[b++] = temp[j] & 0xff;
					this.buf[b++] = (temp[j] >>> 8) & 0xff;
					this.buf[b++] = (temp[j] >>> 16) & 0xff;
					this.buf[b++] = (temp[j] >>> 24) & 0xff;
				}
				this.state[12]++;
				this.counter = 0;
			}
			ciphertext[i] ^= this.buf[this.counter++];
			this.bytesRead++;
			if (this.parts && this.bytesRead >= this.parts[this.part]) {
				this.state[12] = 0;
				this.counter = 0;
				this.bytesRead = 0;
				this.part++;
			}
		}
	}
}
