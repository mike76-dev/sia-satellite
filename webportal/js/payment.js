// This is your publishable API key.
const stripe = Stripe('pk_live_51MB3TeCVw2rpdJXxUHIBPaLJdqBjVCUFKCPLukldKsIiElXqkCBEwhnapR9YC7K5RzCCMiJ6xUsgIyuexy4ZMupJ00bT79EkUF');

// The items the customer wants to buy
const items = [{ id: 'storage' }];

let elements;

// Fetches a payment intent and captures the client secret
async function initialize() {
	document.getElementById('payment-submit').classList.add('disabled');
	document.getElementById('payment-back').classList.add('disabled');
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
			showMessage('Payment succeeded!');
			break;
		case 'processing':
			showMessage('Your payment is processing.');
			break;
		case 'requires_payment_method':
			showMessage('Your payment was not successful, please try again.');
			break;
		default:
			showMessage('Something went wrong.');
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
