document.getElementById('g_id_onload').setAttribute('data-client_id', googleClientID);

function handleCredentialResponse(credentials) {
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
	fetch(apiBaseURL + '/auth/login/google', options)
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
			let loginPassErr = document.getElementById('login-password-error');
            let signupPassErr = document.getElementById('signup-password-error');
			switch (data.code) {
				case 31:
					loginPassErr.innerHTML = 'Too many attempts, try again later';
                    signupPassErr.innerHTML = 'Too many attempts, try again later';
					loginPassErr.classList.remove('invisible');
                    signupPassErr.classList.remove('invisible');
					window.setTimeout(function() {
                        loginPassErr.classList.add('invisible');
                        signupPassErr.classList.add('invisible');
                    }, 3000);
					break;
				default:
                    console.log(data.message);
			}
		})
		.catch(error => console.log(error));
}
