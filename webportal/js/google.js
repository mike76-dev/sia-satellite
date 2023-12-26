document.getElementById('g_id_onload').setAttribute('data-client_id', googleClientID);

function handleCredentialResponse(credentials) {
	let a = document.getElementById('signup-agree');
	if (status == 'signup' && !a.checked) {
		let err = document.getElementById('signup-agree-error');
		err.innerHTML = 'Please agree to the Terms and the Policy';
		err.classList.remove('invisible');
		return;
	}
    let data = {
		clientId:   credentials.clientId,
        credential: credentials.credential
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type':       'application/json;charset=utf-8',
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/login/google?action=' + status, options)
		.then(response => {
			if (response.status == 204) {
				setStatus('');
				let m = document.getElementById('message');
				m.innerHTML = 'Congratulations, you are logged in!';
				m.classList.remove('disabled');
				window.setTimeout(function() {
					let i = window.location.href.lastIndexOf('/');
					window.location.replace(window.location.href.slice(0, i) + '/dashboard.html');
				}, 3000);
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let loginEmailErr = document.getElementById('login-email-error');
			let signupEmailErr = document.getElementById('signup-email-error');
			let loginPassErr = document.getElementById('login-password-error');
            let signupPassErr = document.getElementById('signup-password-error');
			switch (data.code) {
				case 10:
					if (status == 'signup') {
						signupEmailErr.innerHTML = 'Provided email address is invalid';
						signupEmailErr.classList.remove('invisible');
					} else {
						loginEmailErr.innerHTML = 'Provided email address is invalid';
						loginEmailErr.classList.remove('invisible');
					}
					break;
				case 11:
					if (status == 'signup') {
						signupEmailErr.innerHTML = 'Email address is already used';
						signupEmailErr.classList.remove('invisible');
					} else {
						loginEmailErr.innerHTML = 'Email address is already used';
						loginEmailErr.classList.remove('invisible');
					}
					break;
				case 31:
					if (status == 'signup') {
    	                signupPassErr.innerHTML = 'Too many attempts, try again later';
            	        signupPassErr.classList.remove('invisible');
						window.setTimeout(function() {
                        	signupPassErr.classList.add('invisible');
	                    }, 3000);
					} else {
						loginPassErr.innerHTML = 'Too many attempts, try again later';
						loginPassErr.classList.remove('invisible');
						window.setTimeout(function() {
                    	    loginPassErr.classList.add('invisible');
	                    }, 3000);
					}
					break;
				default:
                    console.log(data.message);
			}
			a.checked = false;
		})
		.catch(error => console.log(error));
}
