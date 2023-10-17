var currencies = [];
var fees;
getCurrencies();

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
			let sc = document.getElementById('select');
			currencies.forEach((cur) => {
				let op = document.createElement('option');
				op.value = cur.name;
				op.innerHTML = cur.name;
				sc.appendChild(op);
			});
            getFees();
		})
		.catch(error => console.log(error));
}

function getFees() {
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/fees', options)
		.then(response => response.json())
		.then(data => {
            fees = data;
            renderFees();
        })
		.catch(error => console.log(error));
}

function renderFees() {
    let cur = document.getElementById('select').value;
    let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/averages?currency=' + cur, options)
		.then(response => response.json())
		.then(data => {
            if (data.code) {
                console.log(code);
            } else {
                document.getElementById('form-pp').innerHTML = (fees.formcontract.prepayment*100).toFixed(0) + '%';
                document.getElementById('form-in').innerHTML = (fees.formcontract.invoicing*100).toFixed(0) + '%';
                document.getElementById('save-pp').innerHTML = (fees.savemetadata.prepayment*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('save-in').innerHTML = (fees.savemetadata.invoicing*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('retrieve-pp').innerHTML = (fees.retrievemetadata.prepayment*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('retrieve-in').innerHTML = (fees.retrievemetadata.invoicing*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('store-pp').innerHTML = (fees.storemetadata.prepayment*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('store-in').innerHTML = (fees.storemetadata.invoicing*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('migrate-pp').innerHTML = (fees.migrateslab.prepayment*data.scrate).toPrecision(2) + ' ' + cur;
                document.getElementById('migrate-in').innerHTML = (fees.migrateslab.invoicing*data.scrate).toPrecision(2) + ' ' + cur;
            }
        })
		.catch(error => console.log(error));
}