if (apiBaseURL == '') {
	throw new Error('API base URL not specified');
}

if (!navigator.cookieEnabled) {
	let m = document.getElementById('message');
	m.innerHTML = 'Please allow cookies in your browser and reload the page. <a href="privacy.html" target="_blank">Read more</a>';
	m.classList.remove('disabled');
	throw new Error('Cookies disabled');
}

const specialChars = [
	'`', '~', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')',
	'-', '_', '=', '+', '[', ']', '{', '}', ';', ':', "'", '"',
	'\\', '|', ',', '.', '<', '>', '/', '?'
];

var status = '';

var query = window.location.search;
if (query.startsWith('?token=')) {
	let token = query.slice(7);
	if (token.length == 128) {
		let options = {
			method: 'GET',
			headers: {
				'Content-Type':   'application/json;charset=utf-8',
				'Satellite-Token': token
			}
		}
		let m = document.getElementById('message');
		fetch(apiBaseURL + '/auth', options)
			.then(response => {
				if (response.status == 204) {
					cookie = getCookie('satellite-change');
					if (cookie == '') {
						m.innerHTML = 'Email verification successful, you will be redirected now...';
						m.classList.remove('disabled');
						window.setTimeout(function() {
							m.classList.add('disabled');
							m.innerHTML = '';
							setStatus('login');
						}, 3000);
						return 'request successful';
					} else {
						m.innerHTML = 'You will now be redirected to enter a new password...';
						m.classList.remove('disabled');
						window.setTimeout(function() {
							m.classList.add('disabled');
							m.innerHTML = '';
							setStatus('change');
						}, 3000);
						return response.json();
					}
				} else return response.json();
			})
			.then(data => {
				switch (data.Code) {
					case 40:
						m.innerHTML = 'Provided link is invalid';
						m.classList.remove('disabled');
						break;
					case 41:
						m.innerHTML = 'Provided link already expired. Please request a new one.';
						m.classList.remove('disabled');
						window.setTimeout(function() {
							m.classList.add('disabled');
							m.innerHTML = '';
							setStatus('signup');
						}, 3000);
						break;
					default:
				}
			})
			.catch(error => console.log(error));
	}
} else {
	if (getCookie('satellite') == '') {
		document.getElementById('cookie').classList.remove('disabled');
		setStatus('login');
	} else {
		setStatus('');
		let m = document.getElementById('message');
		m.innerHTML = 'You are logged in, proceeding...';
		m.classList.remove('disabled');
		window.setTimeout(function() {
			m.classList.add('disabled');
			m.innerHTML = '';
			let i = window.location.href.lastIndexOf('/');
			window.location.replace(window.location.href.slice(0, i) + '/dashboard.html');
		}, 3000);
	}
}

function allowCookies() {
	document.getElementById('cookie').classList.add('disabled');
}

function setCookie(name, value, days) {
	let date = new Date();
	date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000));
	document.cookie = name + '=' + value + '; expires=' + date.toUTCString() + '; path=/';
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

function setStatus(s) {
	let login = document.getElementById('login');
	let signup = document.getElementById('signup');
	let reset = document.getElementById('reset');
	let resendVerify = document.getElementById('resend-verify');
	let resendReset = document.getElementById('resend-reset');
	let change = document.getElementById('change');
	status = s;
	clearErrors();
	switch (s) {
		case 'login':
			clearLoginTab();
			login.classList.remove('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.add('disabled');
			change.classList.add('disabled');
			break;
		case 'signup':
			clearSignupTab();
			login.classList.add('disabled');
			signup.classList.remove('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.add('disabled');
			change.classList.add('disabled');
			break;
		case 'reset':
			clearResetTab();
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.remove('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.add('disabled');
			change.classList.add('disabled');
			break;
		case 'resend-verify':
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.remove('disabled');
			resendReset.classList.add('disabled');
			change.classList.add('disabled');
			break;
		case 'resend-reset':
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.remove('disabled');
			change.classList.add('disabled');
			break;
		case 'change':
			clearChangeTab();
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.add('disabled');
			change.classList.remove('disabled');
			break;
		default:
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			resendVerify.classList.add('disabled');
			resendReset.classList.add('disabled');
			change.classList.add('disabled');
	}
}

function clearErrors() {
	document.getElementById('login-email-error').classList.add('invisible');
	document.getElementById('login-password-error').classList.add('invisible');
	document.getElementById('signup-email-error').classList.add('invisible');
	document.getElementById('signup-password-error').classList.add('invisible');
	document.getElementById('signup-retype-error').classList.add('invisible');
	document.getElementById('signup-agree-error').classList.add('invisible');
	document.getElementById('reset-email-error').classList.add('invisible');
	document.getElementById('resend-verify-error').classList.add('invisible');
	document.getElementById('resend-reset-error').classList.add('invisible');
	document.getElementById('change-password-error').classList.add('invisible');
	document.getElementById('change-retype-error').classList.add('invisible');
}

function clearLoginTab() {
	document.getElementById('login-email').value = '';
	document.getElementById('login-password').value = '';
}

function clearSignupTab() {
	document.getElementById('signup-email').value = '';
	document.getElementById('signup-password').value = '';
	document.getElementById('signup-retype').value = '';
	document.getElementById('signup-agree').checked = false;
}

function clearResetTab() {
	document.getElementById('reset-email').value = '';
}

function clearChangeTab() {
	document.getElementById('change-password').value = '';
	document.getElementById('change-retype').value = '';
}

function validateEmail(addr) {
	if (addr == '') return false;
	let at = addr.indexOf('@');
	if (at <= 0) return false;
	addr = addr.slice(at + 1);
	let dot = addr.indexOf('.');
	if (dot <= 0) return false;
	if (dot == addr.length - 1) return false;
	return true;
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

function loginEmailChange() {
	let err = document.getElementById('login-email-error');
	err.classList.add('invisible');
}

function loginPasswordChange() {
	let err = document.getElementById('login-password-error');
	err.classList.add('invisible');
}

function loginClick() {
	let e = document.getElementById('login-email');
	if (!validateEmail(e.value)) {
		let err = document.getElementById('login-email-error');
		err.innerHTML = 'Provided email address is invalid';
		err.classList.remove('invisible');
		return;
	}
	let p = document.getElementById('login-password');
	if (p.value == '') {
		let err = document.getElementById('login-password-error');
		err.innerHTML = 'Provided password is invalid';
		err.classList.remove('invisible');
		return;
	}
	let data = {
		email: e.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type':       'application/json;charset=utf-8',
			'Satellite-Password': p.value
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/login', options)
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
			let emailErr = document.getElementById('login-email-error');
			let passErr = document.getElementById('login-password-error');
			switch (data.Code) {
				case 30:
					passErr.innerHTML = 'Wrong combination of email and password';
					passErr.classList.remove('invisible');
					break;
				case 31:
					emailErr.innerHTML = 'Too many attempts, try again later';
					emailErr.classList.remove('invisible');
					window.setTimeout(function() {emailErr.classList.add('invisible')}, 3000);
					break;
				default:
			}
		})
		.catch(error => console.log(error));
}

function signupEmailChange() {
	let err = document.getElementById('signup-email-error');
	err.classList.add('invisible');
}

function signupPasswordChange() {
	let err = document.getElementById('signup-password-error');
	err.classList.add('invisible');
}

function signupRetypeChange() {
	let err = document.getElementById('signup-retype-error');
	err.classList.add('invisible');
}

function signupAgreeChange() {
	let err = document.getElementById('signup-agree-error');
	err.classList.add('invisible');
}

function signupClick() {
	let e = document.getElementById('signup-email');
	if (!validateEmail(e.value)) {
		let err = document.getElementById('signup-email-error');
		err.innerHTML = 'Provided email address is invalid';
		err.classList.remove('invisible');
		return;
	}
	let p = document.getElementById('signup-password');
	if (!validatePassword(p.value)) {
		let err = document.getElementById('signup-password-error');
		err.innerHTML = 'Provided password is invalid';
		err.classList.remove('invisible');
		return;
	}
	let r = document.getElementById('signup-retype');
	if (r.value != p.value) {
		let err = document.getElementById('signup-retype-error');
		err.innerHTML = 'The two passwords do not match';
		err.classList.remove('invisible');
		return;
	}
	let a = document.getElementById('signup-agree');
	if (!a.checked) {
		let err = document.getElementById('signup-agree-error');
		err.innerHTML = 'Please agree to the Terms and the Policy';
		err.classList.remove('invisible');
		return;
	}
	let data = {
		email: e.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type':       'application/json;charset=utf-8',
			'Satellite-Password': p.value
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/register', options)
		.then(response => {
			if (response.status == 204) {
				let rv = document.getElementById('resend-verify-email');
				rv.value = e.value;
				setStatus('resend-verify');
				clearSignupTab();
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let emailErr = document.getElementById('signup-email-error');
			let passErr = document.getElementById('signup-password-error');
			switch (data.Code) {
				case 10:
					emailErr.innerHTML = 'Provided email address is invalid';
					emailErr.classList.remove('invisible');
					break;
				case 11:
					emailErr.innerHTML = 'Email address is already used';
					emailErr.classList.remove('invisible');
					break;
				case 12:
					emailErr.innerHTML = 'Email address is too long';
					emailErr.classList.remove('invisible');
					break;
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
				case 30:
					passErr.innerHTML = 'Wrong combination of email and password';
					passErr.classList.remove('invisible');
					break;
				case 31:
					emailErr.innerHTML = 'Too many attempts, try again later';
					emailErr.classList.remove('invisible');
					window.setTimeout(function() {emailErr.classList.add('invisible')}, 3000);
					break;
				default:
			}
			a.checked = false;
		})
		.catch(error => console.log(error));
}

function resetEmailChange() {
	let err = document.getElementById('reset-email-error');
	err.classList.add('invisible');
}

function resetClick() {
	let e = document.getElementById('reset-email');
	if (!validateEmail(e.value)) {
		let err = document.getElementById('reset-email-error');
		err.innerHTML = 'Provided email address is invalid';
		err.classList.remove('invisible');
		return;
	}
	let data = {
		email: e.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/reset', options)
		.then(response => {
			if (response.status == 204) {
				let rv = document.getElementById('resend-reset-email');
				rv.value = e.value;
				setStatus('resend-reset');
				clearResetTab();
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let emailErr = document.getElementById('reset-email-error');
			switch (data.Code) {
				case 31:
					emailErr.innerHTML = 'Too many attempts, try again later';
					emailErr.classList.remove('invisible');
					window.setTimeout(function() {emailErr.classList.add('invisible')}, 3000);
					break;
				default:
			}
		})
		.catch(error => console.log(error));
}

function resendVerifyClick() {
	let e = document.getElementById('resend-verify-email');
	let data = {
		email: e.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/register/resend', options)
		.then(response => {
			if (response.status == 204) {
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let resendErr = document.getElementById('resend-verify-error');
			switch (data.Code) {
				case 31:
					resendErr.innerHTML = 'Too many attempts, try again later';
					resendErr.classList.remove('invisible');
					window.setTimeout(function() {resendErr.classList.add('invisible')}, 3000);
					break;
				default:
			}
			return;
		})
		.catch(error => console.log(error));
}

function resendResetClick() {
	let e = document.getElementById('resend-reset-email');
	let data = {
		email: e.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/auth/reset/resend', options)
		.then(response => {
			if (response.status == 204) {
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let resendErr = document.getElementById('resend-reset-error');
			switch (data.Code) {
				case 31:
					resendErr.innerHTML = 'Too many attempts, try again later';
					resendErr.classList.remove('invisible');
					window.setTimeout(function() {resendErr.classList.add('invisible')}, 3000);
					break;
				default:
			}
			return;
		})
		.catch(error => console.log(error));
}

function changePasswordChange() {
	let err = document.getElementById('change-password-error');
	err.classList.add('invisible');
}

function changeRetypeChange() {
	let err = document.getElementById('change-retype-error');
	err.classList.add('invisible');
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
		method: 'GET',
		headers: {
			'Content-Type':       'application/json;charset=utf-8',
			'Satellite-Password': p.value
		}
	}
	let m = document.getElementById('message');
	fetch(apiBaseURL + '/auth/change', options)
		.then(response => {
			if (response.status == 204) {
				setStatus('');
				m.innerHTML = 'Password changed successfully, please log in using your new password...';
				m.classList.remove('disabled');
				deleteCookie('satellite-change');
				window.setTimeout(function() {
					m.classList.add('disabled');
					m.innerHTML = '';
					clearLoginTab();
					setStatus('login');
				}, 3000);
				return 'request successful';
			} else return response.json();
		})
		.then(data => {
			let passErr = document.getElementById('change-password-error');
			switch (data.Code) {
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
					setStatus('');
					m.innerHTML = 'Unknown error. Please request a new link.';
					m.classList.remove('disabled');
					clearResetTab();
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
						setStatus('reset');
					}, 3000);
					break;
				case 41:
					m.innerHTML = 'Provided link already expired. Please request a new one.';
					m.classList.remove('disabled');
					clearResetTab();
					window.setTimeout(function() {
						m.classList.add('disabled');
						m.innerHTML = '';
						setStatus('reset');
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

function toggleLoginPassword() {
	let c = document.getElementById('login-password-toggle');
	let i = document.getElementById('login-password-icon');
	let p = document.getElementById('login-password');
	if (c.checked) {
		p.type = 'text';
		i.src = 'assets/hide-password.png';
	} else {
		p.type = 'password';
		i.src = 'assets/show-password.png';
	}
}

function toggleSignupPassword() {
	let c = document.getElementById('signup-password-toggle');
	let i = document.getElementById('signup-password-icon');
	let p = document.getElementById('signup-password');
	if (c.checked) {
		p.type = 'text';
		i.src = 'assets/hide-password.png';
	} else {
		p.type = 'password';
		i.src = 'assets/show-password.png';
	}
}

function toggleSignupRetype() {
	let c = document.getElementById('signup-retype-toggle');
	let i = document.getElementById('signup-retype-icon');
	let p = document.getElementById('signup-retype');
	if (c.checked) {
		p.type = 'text';
		i.src = 'assets/hide-password.png';
	} else {
		p.type = 'password';
		i.src = 'assets/show-password.png';
	}
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
