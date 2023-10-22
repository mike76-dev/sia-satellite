getAnnouncement();

function getAnnouncement() {
	let a = document.getElementById('announcement-container');
	let path = window.location.pathname;
	path = path.slice(path.lastIndexOf('/') + 1);
	let options = {
		method: 'GET',
		headers: {
			'Content-Type': 'application/json;charset=utf-8'
		}
	}
	fetch(apiBaseURL + '/dashboard/announcement', options)
		.then(response => response.json())
		.then(data => {
			if (data.code) {
				console.log(data.message);
			} else {
				if (data.announcement == "") return;
				document.getElementById('announcement').innerHTML = data.announcement;
				a.classList.remove('disabled');
				if (!data.maintenance || path == 'index.html') {
					a.addEventListener('click', () => {
						a.classList.add('disabled');
					});
				}
			}
		})
		.catch(error => {
			console.log(error);
			document.getElementById('announcement').innerHTML = 'Service temporarily unavailable.'
			a.classList.remove('disabled');
			if (path == 'index.html') {
				a.addEventListener('click', () => {
					a.classList.add('disabled');
				});
			}
		});
}
