if (apiBaseURL == '') {
	throw new Error('API base URL not specified');
}

const specialChars = [
	'`', '~', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')',
	'-', '_', '=', '+', '[', ']', '{', '}', ';', ':', "'", '"',
	'\\', '|', ',', '.', '<', '>', '/', '?'
];

const month = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

if (!navigator.cookieEnabled || getCookie('satellite') == '') {
	let i = window.location.href.lastIndexOf('/');
	window.location.replace(window.location.href.slice(0, i) + '/rent.html');
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

var menu = document.getElementById('menu');
var pages = document.getElementById('pages');
for (let i = 0; i < menu.childElementCount; i++) {
	menu.children[i].addEventListener('click', function(e) {
		setActiveMenuIndex(e.target.getAttribute('index'));
	});
}

setActiveMenuIndex(0);

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

var paymentEstimation;
var paymentAmount = 0.0;
var paymentCurrency = 'USD';
var processing = false;
var paying = false;

getVersion();
retrieveBlockHeight();
retrieveBalance();
retrieveAverages();
window.setInterval(retrieveBlockHeight, 60000);
window.setInterval(retrieveBalance, 60000);
window.setInterval(retrieveAverages, 600000);
retrieveKey();

var payments = [];
var paymentsFrom = 1;
var paymentsStep = 10;
var contracts = [];
var contractsFrom = 1;
var contractsStep = 10;
var files = [];
var selectedFiles = [];
var filesFrom = 1;
var filesStep = 10;

var sortByStart = 'inactive';
var sortByEnd = 'inactive';

var expandedContract = -1;

function setActiveMenuIndex(ind) {
	let li, p;
	if (ind > menu.childElementCount) return;
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

function validatePassword(pass) {
	if (pass.length < 8) return false;
	if (pass.length > 255) return false;
	let l = 0, u = 0, d = 0, s = 0;
	for (let i = 0; i < pass.length; i++) {
		if (/[a-z]/.test(pass[i])) l++;
		if (/[A-Z]/.test(pass[i])) u++;
		if (/[0-9]/.test(pass[i])) d++;
		if (specialChars.includes(pass[i])) s++;
	}
	return l > 0 && u > 0 && d > 0 && s > 0;
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
	if (!validatePassword(p.value)) {
		let err = document.getElementById('change-password-error');
		err.innerHTML = 'Provided password is invalid';
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
				case 22:
					passErr.innerHTML = 'Password is not secure enough';
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
				let bp = document.getElementById('balance-primary');
				let bs = document.getElementById('balance-secondary');
				let lp = document.getElementById('locked-primary');
				let ls = document.getElementById('locked-secondary');
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
					(data.subscribed ? 'Subscription' : 'Pre-payment');
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
				document.getElementById('reveal').classList.remove('disabled');
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
	initialize();
	paying = true;
	document.getElementById('to-pay').innerHTML = paymentAmount.toFixed(2) + ' ' +
		paymentCurrency;
	document.getElementById('select').classList.add('disabled');
	document.getElementById('payment').classList.remove('disabled');
}

function backToSelect() {
	paying = false;
	document.getElementById('payment').classList.add('disabled');
	document.getElementById('select').classList.remove('disabled');
}

function changePaymentsStep(s) {
	paymentsStep = parseInt(s.value);
	paymentsFrom = 1;
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
	let tr;
	payments.forEach((row, i) => {
		if (i < paymentsFrom - 1) return;
		if (i >= paymentsFrom + paymentsStep - 1) return;
		timestamp = new Date(row.timestamp * 1000);
		tr = document.createElement('tr');
		tr.innerHTML = '<td>' + (i + 1) + '</td>';
		tr.innerHTML += '<td>' + timestamp.toLocaleString() + '</td>';
		tr.innerHTML += '<td>' + row.amount.toFixed(2) + '</td>';
		tr.innerHTML += '<td>' + row.currency + '</td>';
		tr.innerHTML += '<td>' + row.amountsc.toFixed(2) + ' SC</td>';
		tbody.appendChild(tr);
	});
	document.getElementById('history-empty').classList.add('disabled');
	document.getElementById('history-non-empty').classList.remove('disabled');
	document.getElementById('history-prev').disabled = paymentsFrom == 1;
	document.getElementById('history-next').disabled = payments.length < paymentsFrom + paymentsStep;
}

function getPayments() {
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
				renderPayments();
			}
		})
		.catch(error => console.log(error));
}

function paymentsPrev() {
	paymentsFrom = paymentsFrom - paymentsStep;
	if (paymentsFrom < 1) paymentsFrom = 1;
	renderPayments();
}

function paymentsNext() {
	paymentsFrom = paymentsFrom + paymentsStep;
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
	let k = document.getElementById('reveal-key');
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/key', options)
		.then(response => response.json())
		.then(data => {
			if (data.key) {
				k.value = data.key;
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
	contractsStep = parseInt(s.value);
	contractsFrom = 1;
	expandedContract = -1;
	renderContracts();
}

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
	let tr;
	contracts.forEach((row, i) => {
		if (i < contractsFrom - 1) return;
		if (i >= contractsFrom + contractsStep - 1) return;
		tr = document.createElement('tr');
		tr.innerHTML = '<td>' + (i + 1) + '</td>';
		tr.innerHTML += '<td class="cell-overflow">' + row.id.slice(row.id.indexOf(':') + 1) + '</td>';
		tr.innerHTML += '<td>' + row.startheight + '</td>';
		tr.innerHTML += '<td>' + row.endheight + '</td>';
		tr.innerHTML += '<td class="cell-overflow">' + row.netaddress + '</td>';
		tr.innerHTML += '<td>' + row.size + '</td>';
		tr.innerHTML += '<td>' + row.totalcost + '</td>';
		tr.innerHTML += '<td>' + row.status + '</td>';
		tr.index = i;
		tr.addEventListener("click", expandContract);
		tbody.appendChild(tr);
	});
	document.getElementById('contracts-empty').classList.add('disabled');
	document.getElementById('contracts-non-empty').classList.remove('disabled');
	document.getElementById('contracts-prev').disabled = contractsFrom == 1;
	document.getElementById('contracts-next').disabled = contracts.length < contractsFrom + contractsStep;
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
	let current = document.getElementById('contracts-current').checked;
	let old = document.getElementById('contracts-old').checked;
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
				return;
			}
			if (data.code) {
				console.log(data);
			} else {
				contracts = data;
				if (sortByStart == 'ascending') {
					contracts = contracts.sort((a, b) => a.startheight - b.startheight);
				}
				if (sortByStart == 'descending') {
					contracts = contracts.sort((a, b) => b.startheight - a.startheight);
				}
				if (sortByEnd == 'ascending') {
					contracts = contracts.sort((a, b) => a.endheight - b.endheight);
				}
				if (sortByEnd == 'decending') {
					contracts = contracts.sort((a, b) => b.endheight - a.endheight);
				}
				renderContracts();
			}
		})
		.catch(error => console.log(error));
}

function contractsPrev() {
	contractsFrom = contractsFrom - contractsStep;
	if (contractsFrom < 1) contractsFrom = 1;
	expandedContract = -1;
	renderContracts();
}

function contractsNext() {
	contractsFrom = contractsFrom + contractsStep;
	expandedContract = -1;
	renderContracts();
}

function contractsChanged() {
	contractsFrom = 1;
	expandedContract = -1;
	getContracts();
}

function sortByContractStart() {
	switch (sortByStart) {
	case 'inactive':
		sortByStart = 'ascending';
		document.getElementById('contracts-start-asc').classList.add('active');
		sortByEnd = 'inactive';
		document.getElementById('contracts-end-desc').classList.remove('active');
		document.getElementById('contracts-end-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.startheight - b.startheight);
		break;
	case 'ascending':
		sortByStart = 'descending';
		document.getElementById('contracts-start-desc').classList.add('active');
		document.getElementById('contracts-start-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => b.startheight - a.startheight);
		break;
	case 'descending':
		sortByStart = 'ascending';
		document.getElementById('contracts-start-asc').classList.add('active');
		document.getElementById('contracts-start-desc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.startheight - b.startheight);
		break;
	default:
	}
	expandedContract = -1;
	renderContracts();
}

function sortByContractEnd() {
	switch (sortByEnd) {
	case 'inactive':
		sortByEnd = 'ascending';
		document.getElementById('contracts-end-asc').classList.add('active');
		sortByStart = 'inactive';
		document.getElementById('contracts-start-desc').classList.remove('active');
		document.getElementById('contracts-start-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.endheight - b.endheight);
		break;
	case 'ascending':
		sortByEnd = 'descending';
		document.getElementById('contracts-end-desc').classList.add('active');
		document.getElementById('contracts-end-asc').classList.remove('active');
		contracts = contracts.sort((a, b) => b.endheight - a.endheight);
		break;
	case 'descending':
		sortByEnd = 'ascending';
		document.getElementById('contracts-end-asc').classList.add('active');
		document.getElementById('contracts-end-desc').classList.remove('active');
		contracts = contracts.sort((a, b) => a.endheight - b.endheight);
		break;
	default:
	}
	expandedContract = -1;
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
				clp.innerHTML = (data.currentlocked * data.scrate).toFixed(2) +
					' ' + averages.currency;
				cls.innerHTML = data.currentlocked.toFixed(2) + ' SC';
				cup.innerHTML = (data.currentused * data.scrate).toFixed(2) +
					' ' + averages.currency;
				cus.innerHTML = data.currentused.toFixed(2) + ' SC';
				cop.innerHTML = (data.currentoverhead * data.scrate).toFixed(2) +
					' ' + averages.currency;
				cos.innerHTML = data.currentoverhead.toFixed(2) + ' SC';
				cf.innerHTML = data.currentformed;
				cr.innerHTML = data.currentrenewed;
				cs.innerHTML = data.currentslabssaved;
				crr.innerHTML = data.currentslabsretrieved;
				cmm.innerHTML = data.currentslabsmigrated;
				plp.innerHTML = (data.prevlocked * data.scrate).toFixed(2) +
					' ' + averages.currency;
				pls.innerHTML = data.prevlocked.toFixed(2) + ' SC';
				pup.innerHTML = (data.prevused * data.scrate).toFixed(2) +
					' ' + averages.currency;
				pus.innerHTML = data.prevused.toFixed(2) + ' SC';
				pop.innerHTML = (data.prevoverhead * data.scrate).toFixed(2) +
					' ' + averages.currency;
				pos.innerHTML = data.prevoverhead.toFixed(2) + ' SC';
				pf.innerHTML = data.prevformed;
				pr.innerHTML = data.prevrenewed;
				ps.innerHTML = data.prevslabssaved;
				prr.innerHTML = data.prevslabsretrieved;
				pmm.innerHTML = data.prevslabsmigrated;
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
				ar.checked = data.autorenew;
				md.checked = data.backupmetadata;
				fr.checked = data.autorepair;
			}
		})
		.catch(error => console.log(error));
}

function changeFilesStep(s) {
	filesStep = parseInt(s.value);
	filesFrom = 1;
	renderFiles();
}

function renderFiles() {
	let tbody = document.getElementById('files-table');
	tbody.innerHTML = '';
	if (files.length == 0) {
		document.getElementById('files-non-empty').classList.add('disabled');
		document.getElementById('files-empty').classList.remove('disabled');
		document.getElementById('files-prev').disabled = true;
		document.getElementById('files-next').disabled = true;
		filesFrom = 1;
		return;
	}
	let tr;
	files.forEach((row, i) => {
		if (i < filesFrom - 1) return;
		if (i >= filesFrom + filesStep - 1) return;
		timestamp = new Date(row.uploaded * 1000);
		tr = document.createElement('tr');
		tr.innerHTML = '<td><label class="checkbox" style="margin-left: 0.5rem"><input type="checkbox" onchange="selectFile(this)"><span class="checkmark"></span></label></td>';
		tr.innerHTML += '<td>' + (i + 1) + '</td>';
		tr.innerHTML += '<td class="cell-overflow">' + row.path.slice(1) + '</td>';
		tr.innerHTML += '<td>' + row.slabs + '</td>';
		tr.innerHTML += '<td>' + timestamp.toLocaleString() + '</td>';
		tr.children[0].children[0].children[0].setAttribute('index', i);
		tr.children[0].children[0].children[0].checked = selectedFiles.includes(i);
		tbody.appendChild(tr);
	});
	document.getElementById('files-empty').classList.add('disabled');
	document.getElementById('files-non-empty').classList.remove('disabled');
	document.getElementById('files-prev').disabled = filesFrom == 1;
	document.getElementById('files-next').disabled = files.length < filesFrom + filesStep;
}

function selectFile(obj) {
	let index = parseInt(obj.getAttribute('index'));
	let i = selectedFiles.indexOf(index);
	if (i < 0) {
		selectedFiles.push(index);
	} else {
		selectedFiles.splice(i, 1);
	}
	document.getElementById("files-delete").disabled = selectedFiles.length == 0;
	document.getElementById('files-all').checked = selectedFiles.length == files.length;
}

function selectFiles() {
	if (selectedFiles.length === files.length) {
		selectedFiles = [];
	} else {
		selectedFiles = [];
		files.forEach((value, index) => {
			selectedFiles.push(index);
		})
	}
	document.getElementById("files-delete").disabled = selectedFiles.length == 0;
	renderFiles();
}

function getFiles() {
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
				renderFiles();
			}
		})
		.catch(error => console.log(error));
}

function filesPrev() {
	filesFrom = filesFrom - filesStep;
	if (filesFrom < 1) filesFrom = 1;
	renderFiles();
}

function filesNext() {
	filesFrom = filesFrom + filesStep;
	renderFiles();
}

function deleteFiles() {
	let data = {
		files: selectedFiles
	}
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
				selectedFiles = [];
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