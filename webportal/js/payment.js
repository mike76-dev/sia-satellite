// This is your publishable Stripe API key.
const stripe = Stripe(stripePublicKey);

let elements;

// Fetches a payment intent and captures the client secret
async function initialize(def) {
	document.getElementById('payment-submit').classList.add('disabled');
	document.getElementById('payment-back').classList.add('disabled');
	document.getElementById('payment-header').innerHTML = 
		def ? 'You need to set up the default payment method. For this you will be charged the minimum amount of' :
		'You are going to make a payment of';
	let amount = (def ? 'default:' : '') + paymentAmount.toFixed(2) + paymentCurrency;
	let items = [{ id: amount }];
	const response = await fetch(apiBaseURL + '/stripe/create-payment-intent', {
		method:  'POST',
		headers: { 'Content-Type': 'application/json' },
		body:    JSON.stringify({ items }),
	});
	const { clientSecret } = await response.json();

	const appearance = {
		theme: 'stripe',
		variables: {
			colorText:            '#cfcfcf',
			colorTextPlaceholder: '#7f7f7f',
			colorTextSecondary:   '#7f7f7f',
		},
		rules: {
			'.CheckboxInput': {
				borderColor: '#7f7f7f',
			},
			'.CheckboxLabel': {
				color: '#7f7f7f',
			},
			'.Input': {
				color: '#000000',
			},
		}
	};
	elements = stripe.elements({ appearance, clientSecret });

	const paymentElementOptions = {
		layout: 'tabs',
	};

	const paymentElement = elements.create('payment', paymentElementOptions);
	paymentElement.on('ready', function() {
		document.getElementById('payment-submit').classList.remove('disabled');
		document.getElementById('payment-back').classList.remove('disabled');
	});
	
	paymentElement.mount('#payment-element');
}

async function handleSubmit(e) {
	e.preventDefault();
	setLoading(true);
	document.getElementById('payment-back').classList.add('disabled');

	const { error } = await stripe.confirmPayment({
		elements,
		confirmParams: {
			return_url: window.location.href,
		},
	});

	// This point will only be reached if there is an immediate error when
	// confirming the payment. Otherwise, your customer will be redirected to
	// your `return_url`. For some payment methods like iDEAL, your customer will
	// be redirected to an intermediate site first to authorize the payment, then
	// redirected to the `return_url`.
	if (error.type === 'card_error' || error.type === 'validation_error') {
		showMessage(error.message);
	} else {
		showMessage('An unexpected error occurred.');
	}

	setLoading(false);
	document.getElementById('payment-back').classList.remove('disabled');
}

// Fetches the payment intent status after payment submission
async function checkStatus() {
	const clientSecret = new URLSearchParams(window.location.search).get(
		'payment_intent_client_secret'
	);

	if (!clientSecret) {
		return;
	}

	const { paymentIntent } = await stripe.retrievePaymentIntent(clientSecret);

	switch (paymentIntent.status) {
		case 'succeeded':
			showTextAndReload('Payment succeeded!');
			break;
		case 'processing':
			showTextAndReload('Your payment is processing.');
			break;
		case 'requires_payment_method':
			showTextAndReload('Your payment was not successful, please try again.');
			break;
		default:
			showTextAndReload('Something went wrong.');
			break;
	}
}

// ------- UI helpers -------

function showMessage(messageText) {
	const messageContainer = document.querySelector('#payment-message');

	messageContainer.classList.remove('disabled');
	messageContainer.textContent = messageText;

	setTimeout(function () {
		messageContainer.classList.add('disabled');
		messageText.textContent = '';
	}, 4000);
}

function showTextAndReload(text) {
	const textContainer = document.querySelector('#payment-result');
	textContainer.innerHTML = text;
	textContainer.classList.remove('disabled');
	setTimeout(() => {
		let i = window.location.href.indexOf('?');
		if (i >= 0) {
			window.location.replace(window.location.href.slice(0, i));
		}
	}, 4000);
}

// Show a spinner on payment submission
function setLoading(isLoading) {
	if (isLoading) {
		// Disable the button and show a spinner
		document.querySelector('#payment-submit').disabled = true;
		document.querySelector('#spinner').classList.remove('disabled');
		document.querySelector('#payment-text').classList.add('disabled');
	} else {
		document.querySelector('#payment-submit').disabled = false;
		document.querySelector('#spinner').classList.add('disabled');
		document.querySelector('#payment-text').classList.remove('disabled');
	}
}

checkStatus();
