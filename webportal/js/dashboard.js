if (apiBaseURL == '') {
	throw new Error('API base URL not specified');
}

const specialChars = [
	'`', '~', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')',
	'-', '_', '=', '+', '[', ']', '{', '}', ';', ':', "'", '"',
	'\\', '|', ',', '.', '<', '>', '/', '?'
];

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
	sectorAccessPrice: 0.0
}

var paymentEstimation;
var paymentAmount;
var paymentCurrency;

retrieveBalance();
retrieveAverages();
window.setInterval(retrieveBalance, 300000);
window.setInterval(retrieveAverages, 600000);
retrieveKey();

var paymentsFrom = 1;
var paymentsStep = 10;
var contractsFrom = 1;
var contractsStep = 10;

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
	if (ind == 4) {
		getPayments();
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
				deleteCookie('satellite');
				let i = window.location.href.lastIndexOf('/');
				window.location.replace(window.location.href.slice(0, i) + '/rent.html');
				return 'request successful';
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
			if (data.code) console.log(data)
			else {
				let b = document.getElementById('balance');
				let c = data.currency == '' ? 'USD' : data.currency;
				b.innerHTML = data.balance.toFixed(2) + ' ' + c;
				averages.currency = c;
				if (data.isuser) {
					message = 'Your payment plan: ' + (data.subscribed ? 'Subscription' : 'Pre-payment');
					message += '<br>Remaining balance: ' + data.balance.toFixed(2) + ' ' + c;
					document.getElementById('select-info').innerHTML = message;
					document.getElementById('select-currency').value = c;
					document.getElementById('reveal').classList.remove('disabled');
				}
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

				document.getElementById('numhosts').innerHTML = data.numhosts;
				document.getElementById('storage').innerHTML = data.storageprice.toPrecision(2) +
					' ' + averages.currency + '/TiB/month';
				document.getElementById('upload').innerHTML = data.uploadbandwidthprice.toPrecision(2) +
					' ' + averages.currency + '/TiB';
				document.getElementById('download').innerHTML = data.downloadbandwidthprice.toPrecision(2) +
					' ' + averages.currency + '/TiB';
				document.getElementById('duration').innerHTML = data.duration;
				document.getElementById('limits-contract-average').innerHTML =
					'Average: ' + data.contractprice.toPrecision(2) + ' ' + averages.currency;
				document.getElementById('limits-storage-average').innerHTML =
					'Average: ' + data.storageprice.toPrecision(2) + ' ' + averages.currency;
				document.getElementById('limits-upload-average').innerHTML =
					'Average: ' + data.uploadbandwidthprice.toPrecision(2) + ' ' + averages.currency;
				document.getElementById('limits-download-average').innerHTML =
					'Average: ' + data.downloadbandwidthprice.toPrecision(2) + ' ' + averages.currency;
			}
		})
		.catch(error => console.log(error));
}

function changeCurrency(s) {
	averages.currency = s.value;
	retrieveAverages();
}

function changeInput(obj, check = true) {
	if (check) {
		let v = parseFloat(obj.value)
		if (isNaN(v) || v <= 0) {
			obj.classList.add('content-error');
		} else {
			obj.classList.remove('content-error');
		}
		updateEstimation();
	}
	document.getElementById('payment-amount').classList.add('disabled');
}

function updateEstimation() {
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
		isNaN(redundancy) || redundancy <= 0) {
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
	updateEstimation();
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
	document.getElementById('payment-calculate').disabled = true;
	document.getElementById('calculate-text').classList.add('disabled');
	document.getElementById('calculate-spinner').classList.remove('disabled');
	document.getElementById('payment-amount').classList.add('disabled');
	fetch(apiBaseURL + '/dashboard/hosts', options)
		.then(response => {
			document.getElementById('payment-calculate').disabled = false;
			document.getElementById('calculate-text').classList.remove('disabled');
			document.getElementById('calculate-spinner').classList.add('disabled');
			return response.json();
		})
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				let text = document.getElementById('amount-text');
				let button = document.getElementById('amount-proceed');
				paymentAmount = data.estimation;
				paymentCurrency = data.currency;
				if (data.numhosts == hosts) {
					text.innerHTML = 'Amount to pay: ' + paymentAmount.toFixed(2) + ' ' + paymentCurrency;
					button.innerHTML = 'Proceed to Payment';
				} else {
					text.innerHTML = 'Warning: only ' + data.numhosts +
						' hosts found that match these conditions. Amount to pay: ' +
						paymentAmount.toFixed(2) + ' ' + paymentCurrency;
					button.innerHTML = 'Proceed Anyway';
				}
				document.getElementById('payment-amount').classList.remove('disabled');
			}
		})
		.catch(error => console.log(error));
}

function toPayment() {
	initialize();
	document.getElementById('to-pay').innerHTML = paymentAmount.toFixed(2) + ' ' +
		paymentCurrency;
	document.getElementById('select').classList.add('disabled');
	document.getElementById('payment').classList.remove('disabled');
}

function backToSelect() {
	document.getElementById('payment').classList.add('disabled');
	document.getElementById('select').classList.remove('disabled');
}

function changePaymentsStep(s) {
	paymentsStep = parseInt(s.value);
	getPayments();
}

function getPayments() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/payments?from=' + paymentsFrom + '&to=' + (paymentsFrom + paymentsStep - 1), options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				tbody = document.getElementById('history-table');
				tbody.innerHTML = '';
				if (data.length > 0 || paymentsFrom > 1) {
					document.getElementById('history-empty').classList.add('disabled');
					document.getElementById('history-non-empty').classList.remove('disabled');
				}
				if (data.length > 0) {
					let tr;
					data.forEach((row, i) => {
						timestamp = new Date(row.timestamp * 1000);
						tr = document.createElement('tr');
						tr.innerHTML = '<td>' + (i + paymentsFrom) + '</td>';
						tr.innerHTML += '<td>' + timestamp.toLocaleString() + '</td>';
						tr.innerHTML += '<td>' + row.amount.toFixed(2) + '</td>';
						tr.innerHTML += '<td>' + row.currency + '</td>';
						tr.innerHTML += '<td>' + row.amountusd.toFixed(2) + ' USD</td>';
						tbody.appendChild(tr);
					});
					document.getElementById('history-next').disabled = data.length != paymentsStep;
				} else {
					if (paymentsFrom > 1) {
						paymentsFrom = paymentsFrom - paymentsStep;
						if (paymentsFrom < 1) paymentsFrom = 1;
						if (paymentsFrom == 1) {
							document.getElementById('history-prev').disabled = true;
						}
					}
					document.getElementById('history-next').disabled = true;
				}
			}
		})
		.catch(error => console.log(error));
}

function paymentsPrev() {
	paymentsFrom = paymentsFrom - paymentsStep;
	if (paymentsFrom < 1) paymentsFrom = 1;
	if (paymentsFrom == 1) {
		document.getElementById('history-prev').disabled = true;
	}
	document.getElementById('history-next').disabled = false;
	getPayments();
}

function paymentsNext() {
	paymentsFrom = paymentsFrom + paymentsStep;
	document.getElementById('history-prev').disabled = false;
	getPayments();
}

function revealSeed() {
	b = document.getElementById('reveal-button');
	t = document.getElementById('reveal-text');
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
	k = document.getElementById('reveal-key');
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
				k.innerHTML = data.key;
			}
		})
		.catch(error => console.log(error));
}

function changeContractsStep(s) {
	contractsStep = parseInt(s.value);
	getContracts();
}

function getContracts() {
	let active = document.getElementById('contracts-active').checked;
	let passive = document.getElementById('contracts-passive').checked;
	let refreshed = document.getElementById('contracts-refreshed').checked;
	let disabled = document.getElementById('contracts-disabled').checked;
	let expired = document.getElementById('contracts-expired').checked;
	let exref = document.getElementById('contracts-exref').checked;
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/contracts?from=' + contractsFrom + '&to=' + (contractsFrom + contractsStep - 1) + '&active=' + active + '&passive=' + passive + '&refreshed=' + refreshed + '&disabled=' + disabled + '&expired=' + expired + '&expired-refreshed=' +exref, options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data);
			} else {
				tbody = document.getElementById('contracts-table');
				tbody.innerHTML = '';
				if (data.length > 0 || contractsFrom > 1) {
					document.getElementById('contracts-empty').classList.add('disabled');
					document.getElementById('contracts-non-empty').classList.remove('disabled');
				}
				if (data.length > 0) {
					let tr;
					data.forEach((row, i) => {
						tr = document.createElement('tr');
						tr.innerHTML = '<td>' + (i + contractsFrom) + '</td>';
						tr.innerHTML += '<td class="cell-overflow">' + row.id + '</td>';
						tr.innerHTML += '<td>' + row.startheight + '</td>';
						tr.innerHTML += '<td>' + row.endheight + '</td>';
						tr.innerHTML += '<td class="cell-overflow">' + row.netaddress + '</td>';
						tr.innerHTML += '<td>' + row.size + '</td>';
						tr.innerHTML += '<td>' + row.totalcost + '</td>';
						tr.innerHTML += '<td>' + row.status + '</td>';
						tbody.appendChild(tr);
					});
					document.getElementById('contracts-next').disabled = data.length != contractsStep;
				} else {
					if (contractsFrom > 1) {
						contractsFrom = contractsFrom - contractsStep;
						if (contractsFrom < 1) contractsFrom = 1;
						if (contractsFrom == 1) {
							document.getElementById('contracts-prev').disabled = true;
						}
					}
					document.getElementById('contracts-next').disabled = true;
				}
			}
		})
		.catch(error => console.log(error));
}

function contractsPrev() {
	contractsFrom = contractsFrom - contractsStep;
	if (contractsFrom < 1) contractsFrom = 1;
	if (contractsFrom == 1) {
		document.getElementById('contracts-prev').disabled = true;
	}
	document.getElementById('contracts-next').disabled = false;
	getContracts();
}

function contractsNext() {
	contractsFrom = contractsFrom + contractsStep;
	document.getElementById('contracts-prev').disabled = false;
	getContracts();
}

function contractsChanged() {
	contractsFrom = 1;
	document.getElementById('contracts-empty').classList.remove('disabled');
	document.getElementById('contracts-non-empty').classList.add('disabled');
	getContracts();
}
