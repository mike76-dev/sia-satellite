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
