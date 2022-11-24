if (apiBaseURL == '') {
	throw new Error('API base URL not specified');
}

const specialChars = [
	'`', '~', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')',
	'-', '_', '=', '+', '[', ']', '{', '}', ';', ':', "'", '"',
	'\\', '|', ',', '.', '<', '>', '/', '?'
];

var status = 'login';

function setStatus(s) {
	let login = document.getElementById('login');
	let signup = document.getElementById('signup');
	let reset = document.getElementById('reset');
	status = s;
	switch (s) {
		case 'login':
			login.classList.remove('disabled');
			signup.classList.add('disabled');
			reset.classList.add('disabled');
			break;
		case 'signup':
			login.classList.add('disabled');
			signup.classList.remove('disabled');
			reset.classList.add('disabled');
			break;
		case 'reset':
			login.classList.add('disabled');
			signup.classList.add('disabled');
			reset.classList.remove('disabled');
			break;
		default:
	}
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
		email:    e.value,
		password: p.value
	}
	let options = {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		},
		body: JSON.stringify(data)
	}
	fetch(apiBaseURL + '/authme', options)
		.then(response => {
			if (response.status == 204) {
				return "request successful";
			} else return response.json();
		})
		.then(data => console.log(data)) //TODO
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
}
