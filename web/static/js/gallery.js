// --- 1. View Toggle Logic (Grid vs List) ---
function toggleView(type) {
    const wrapper = document.getElementById('wrapper');
    const listBtn = document.getElementById('listBtn');
    const gridBtn = document.getElementById('gridBtn');

    if (type === 'grid') {
        wrapper.classList.add('grid-view');
        gridBtn.classList.add('active');
        listBtn.classList.remove('active');
    } else {
        wrapper.classList.remove('grid-view');
        listBtn.classList.add('active');
        gridBtn.classList.remove('active');
    }
}

// --- 2. Search Filter Logic (Instant Search) ---
function filterVideos() {
    const input = document.getElementById('searchInput');
    const filter = input.value.toLowerCase();
    const table = document.getElementById('video-table');
    const rows = table.getElementsByTagName('tr');

    // Start at i=1 to skip the table header row
    for (let i = 1; i < rows.length; i++) {
        const titleCell = rows[i].getElementsByTagName('td')[1];
        if (titleCell) {
            const titleText = titleCell.textContent || titleCell.innerText;
            if (titleText.toLowerCase().indexOf(filter) > -1) {
                rows[i].style.display = "";
            } else {
                rows[i].style.display = "none";
            }
        }
    }
}

// --- 3. Marketing & Share Modal Logic ---
let currentUrl = "";
let currentTitle = "";

function openShareModal(title, url) {
    currentUrl = url;
    currentTitle = title;
    
    // Update the input field with the Vidify App link
    document.getElementById('shareUrlInput').value = url;
    
    // Social Sharing Links
    const encodedUrl = encodeURIComponent(url);
    const encodedText = encodeURIComponent("Check out this video: " + title);
    
    document.getElementById('shareX').href = "https://twitter.com/intent/tweet?text=" + encodedText + "&url=" + encodedUrl;
    document.getElementById('shareLI').href = "https://www.linkedin.com/sharing/share-offsite/?url=" + encodedUrl;
    
    // Show the modal
    document.getElementById('shareModal').style.display = 'flex';
}

function closeModal() {
    document.getElementById('shareModal').style.display = 'none';
}

// Marketing Feature: Copy Embed Code for Landing Pages
function copyEmbed() {
    const embedCode = '<iframe src="' + currentUrl + '" width="640" height="360" frameborder="0" allowfullscreen></iframe>';
    navigator.clipboard.writeText(embedCode).then(() => {
        alert("✅ Embed code copied! You can now paste this into your landing page or website.");
    });
}

// Marketing Feature: Copy Email Campaign Link
function copyEmailLink() {
    const thumbUrl = currentUrl.replace('_processed.mp4', '_thumb.jpg');
    const emailHtml = '<a href="' + currentUrl + '"><img src="' + thumbUrl + '" width="300" alt="Watch Video" /></a>';
    navigator.clipboard.writeText(emailHtml).then(() => {
        alert("✅ Email Campaign Link copied! Paste this into your email marketing tool.");
    });
}