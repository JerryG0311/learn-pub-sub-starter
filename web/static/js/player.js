const shareUI = {
    modal: () => document.getElementById('shareModal'),
    shareUrlInput: () => document.getElementById('shareUrlInput'),
    embedCodeInput: () => document.getElementById('embedCodeInput'),
    playerLinkInput: () => document.getElementById('playerLinkInput'),
    shareX: () => document.getElementById('shareX'),
    shareLI: () => document.getElementById('shareLI'),
    copyBtn: () => document.getElementById('copyBtn'),
    linkSection: () => document.getElementById('linkSection'),
    embedSection: () => document.getElementById('embedSection'),
    playerSection: () => document.getElementById('playerSection'),
    linkTabBtn: () => document.getElementById('linkTabBtn'),
    embedTabBtn: () => document.getElementById('embedTabBtn'),
    playerTabBtn: () => document.getElementById('playerTabBtn')
};
let currentTab = 'link';
        function openShareModal(title) {
            const origin = window.location.origin;
            const shareUrl = `${origin}/view/${currentVideoId}`;
            const playerUrl = `${origin}/player/${currentVideoId}`;
            const embedCode = `<iframe src="${playerUrl}" width="100%" height="600" frameborder="0" allow="autoplay; fullscreen" allowfullscreen></iframe>`;
            shareUI.modal().style.display = 'flex';
            shareUI.shareUrlInput().value = shareUrl;
            shareUI.embedCodeInput().value = embedCode;
            shareUI.playerLinkInput().value = playerUrl;

            shareUI.shareX().href = `https://twitter.com/intent/tweet?text=Check out ${encodeURIComponent(title)}&url=${encodeURIComponent(shareUrl)}`;
            shareUI.shareLI().href = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(shareUrl)}`;

            shareUI.shareX().onclick = () => sendAnalyticsPing('share-click', currentVideoId);
            shareUI.shareLI().onclick = () => sendAnalyticsPing('share-click', currentVideoId);
            switchTab('link');
        }
        function switchTab(tab) {
            currentTab = tab;
            shareUI.linkSection().style.display = tab === 'link' ? 'block' : 'none';
            shareUI.embedSection().style.display = tab === 'embed' ? 'block' : 'none';
            shareUI.playerSection().style.display = tab === 'player' ? 'block' : 'none';

            shareUI.linkTabBtn().className = tab === 'link' ? 'tab-btn active' : 'tab-btn inactive';
            shareUI.embedTabBtn().className = tab === 'embed' ? 'tab-btn active' : 'tab-btn inactive';
            shareUI.playerTabBtn().className = tab === 'player' ? 'tab-btn active' : 'tab-btn inactive';
            const copyBtn = shareUI.copyBtn();
            if (copyBtn) {
                if (tab === 'link') copyBtn.innerText = 'Copy Share Link';
                else if (tab === 'embed') copyBtn.innerText = 'Copy Embed Code';
                else copyBtn.innerText = 'Copy Player Link';
            }
        }
        function copyToClipboard() {
            const inputId = currentTab === 'link'
                ? 'shareUrlInput'
                : currentTab === 'embed'
                    ? 'embedCodeInput'
                    : 'playerLinkInput';
            const copyText = document.getElementById(inputId);
            navigator.clipboard.writeText(copyText.value);
            sendAnalyticsPing('share-click', currentVideoId);
            const btn = shareUI.copyBtn();
            btn.innerText = "Copied!";
            setTimeout(() => {
                if (currentTab === 'link') btn.innerText = 'Copy Share Link';
                else if (currentTab === 'embed') btn.innerText = 'Copy Embed Code';
                else btn.innerText = 'Copy Player Link';
            }, 2000);
        }
        function closeModal() { shareUI.modal().style.display = 'none'; }

        const currentVideoId = window.currentVideoId || '';

        const timedCTAs = JSON.parse(document.getElementById('timed-ctas-json')?.textContent || '[]');
        function trackRetention() {
            // no-op fallback to prevent player JS from breaking when retention tracking isn't wired on this page
        }

        function normalizeCTAText(value) {
            if (typeof value !== 'string') return '';
            return value.replace(/^"+|"+$/g, '').trim();
        }

        function normalizeCTAURL(value) {
            if (typeof value !== 'string') return '';
            const cleaned = value.replace(/^"+|"+$/g, '').trim();
            if (!cleaned || cleaned === '__email_capture__') return '';
            return cleaned;
        }

        function getDismissedCTAIds(stage) {
            const raw = stage?.dataset?.dismissedCtaIds || '';
            return new Set(raw.split(',').map((id) => id.trim()).filter(Boolean));
        }

        function setDismissedCTAId(stage, ctaId) {
            if (!stage || !ctaId) return;
            const dismissed = getDismissedCTAIds(stage);
            dismissed.add(ctaId);
            stage.dataset.dismissedCtaIds = Array.from(dismissed).join(',');
        }

        function resetStageCTAUI(stage, ui = null) {
            if (!stage) return;

            const ctaOverlay = ui?.ctaOverlay || stage.querySelector('.video-cta-overlay');
            const legacyContent = ui?.legacyContent || stage.querySelector('.video-cta-overlay > .video-cta-content');
            const ctaButton = ui?.ctaButton || stage.querySelector('.video-cta-button');
            const ctaForm = ui?.ctaForm || stage.querySelector('.video-cta-form');
            const gateForm = ui?.gateForm || stage.querySelector('.video-gate-form');

            stage.classList.remove('show-cta');
            stage.classList.remove('hide-cta-play-badge');
            stage.classList.remove('is-gated');

            if (ctaOverlay) {
                ctaOverlay.classList.remove('has-inline-form');
            }

            if (legacyContent) {
                legacyContent.style.display = '';
            }

            if (ctaButton) {
                ctaButton.style.display = '';
            }

            if (ctaForm) {
                ctaForm.style.display = 'none';
                ctaForm.classList.remove('is-gate-layout');
                ctaForm.dataset.ctaType = 'email';
            }

            if (gateForm) {
                gateForm.style.display = 'none';
                gateForm.classList.remove('is-gate-layout');
            }
        }

        function renderGateCTA(stage, activeCTA, ui = null) {
            if (!stage || !activeCTA) return;

            const ctaOverlay = ui?.ctaOverlay || stage.querySelector('.video-cta-overlay');
            const legacyContent = ui?.legacyContent || stage.querySelector('.video-cta-overlay > .video-cta-content');
            const ctaButton = ui?.ctaButton || stage.querySelector('.video-cta-button');
            const ctaForm = ui?.ctaForm || stage.querySelector('.video-cta-form');
            const gateForm = ui?.gateForm || stage.querySelector('.video-gate-form');
            const gateTitle = ui?.gateTitle || stage.querySelector('.gate-form-title');
            const gateSubtext = ui?.gateSubtext || stage.querySelector('.gate-form-subtext');
            const gateSubmit = ui?.gateSubmit || stage.querySelector('.video-gate-submit');
            const video = ui?.video || stage.querySelector('video');

            if (ctaOverlay) {
                ctaOverlay.classList.remove('has-inline-form');
            }

            if (legacyContent) {
                legacyContent.style.display = 'none';
            }

            if (ctaButton) {
                ctaButton.style.display = 'none';
            }

            if (ctaForm) {
                ctaForm.style.display = 'none';
                ctaForm.classList.remove('is-gate-layout');
            }

            if (gateForm) {
                gateForm.style.display = 'block';
                gateForm.dataset.ctaType = 'email_gate';
                gateForm.classList.add('is-gate-layout');

                const nameInput = gateForm.querySelector('input[name="name"]');
                const emailInput = gateForm.querySelector('input[name="email"]');
                if (nameInput) {
                    nameInput.style.display = 'block';
                    nameInput.required = true;
                }
                if (emailInput) {
                    emailInput.style.display = 'block';
                    emailInput.required = true;
                }

                const formMessage = gateForm.querySelector('.video-lead-message');
                if (formMessage) {
                    formMessage.style.display = 'none';
                }
            }

            if (gateTitle) {
                gateTitle.textContent = 'Ready to take the next step?';
            }

            if (gateSubtext) {
                gateSubtext.textContent = 'Enter your name and email below to unlock the full video.';
            }

            if (gateSubmit) {
                gateSubmit.textContent = 'Get Access';
            }

            stage.classList.add('show-cta');
            stage.classList.add('hide-cta-play-badge');

            if (stage.dataset.gateUnlocked !== 'true') {
                stage.classList.add('is-gated');
                if (video) video.pause();
            } else {
                stage.classList.remove('is-gated');
            }
        }

        function renderTimedEmailCTA(stage, activeCTA, ui = null) {
            if (!stage || !activeCTA) return;

            const ctaOverlay = ui?.ctaOverlay || stage.querySelector('.video-cta-overlay');
            const legacyContent = ui?.legacyContent || stage.querySelector('.video-cta-overlay > .video-cta-content');
            const ctaButton = ui?.ctaButton || stage.querySelector('.video-cta-button');
            const ctaForm = ui?.ctaForm || stage.querySelector('.video-cta-form');
            const timedTitle = ui?.timedTitle || stage.querySelector('.timed-cta-form-title');
            const timedSubtext = ui?.timedSubtext || stage.querySelector('.timed-cta-form-subtext');
            const ctaSubmit = ui?.ctaSubmit || stage.querySelector('.video-cta-submit');
            const gateForm = ui?.gateForm || stage.querySelector('.video-gate-form');

            const cleanHeroText = normalizeCTAText(activeCTA.heroText || '');
            const cleanText = normalizeCTAText(activeCTA.text || 'Get Access');
            const ctaType = activeCTA.ctaType || 'email';

            if (ctaOverlay) {
                ctaOverlay.classList.add('has-inline-form');
            }

            if (legacyContent) {
                legacyContent.style.display = 'none';
            }

            if (ctaButton) {
                ctaButton.style.display = 'none';
            }

            if (ctaForm) {
                ctaForm.style.display = 'block';
                ctaForm.dataset.ctaType = ctaType;
                ctaForm.classList.remove('is-gate-layout');

                const nameInput = ctaForm.querySelector('input[name="name"]');
                const emailInput = ctaForm.querySelector('input[name="email"]');
                if (nameInput) {
                    nameInput.style.display = 'block';
                    nameInput.required = true;
                }
                if (emailInput) {
                    emailInput.style.display = 'block';
                    emailInput.required = true;
                }

                const formMessage = ctaForm.querySelector('.video-lead-message');
                if (formMessage) {
                    formMessage.style.display = 'none';
                }
            }

            if (gateForm) {
                gateForm.style.display = 'none';
                gateForm.classList.remove('is-gate-layout');
            }

            if (timedTitle) {
                timedTitle.innerHTML = '';
                timedTitle.textContent = cleanHeroText || 'Get access to this free guide';
            }

            if (timedSubtext) {
                timedSubtext.innerHTML = '';
                timedSubtext.textContent = 'Enter your first name and email below to continue.';
            }

            if (ctaSubmit) {
                ctaSubmit.textContent = cleanText || 'Get Access';
            }

            stage.classList.add('show-cta');
            stage.classList.add('hide-cta-play-badge');
            stage.classList.remove('is-gated');
        }

        function renderButtonCTA(stage, activeCTA, ui = null) {
            if (!stage || !activeCTA) return;

            const ctaOverlay = ui?.ctaOverlay || stage.querySelector('.video-cta-overlay');
            const legacyContent = ui?.legacyContent || stage.querySelector('.video-cta-overlay > .video-cta-content');
            const ctaButton = ui?.ctaButton || stage.querySelector('.video-cta-button');
            const ctaForm = ui?.ctaForm || stage.querySelector('.video-cta-form');
            const gateForm = ui?.gateForm || stage.querySelector('.video-gate-form');
            const ctaText = ui?.ctaText || (legacyContent ? legacyContent.querySelector('.video-cta-text') : null);

            const cleanHeroText = normalizeCTAText(activeCTA.heroText || '');
            const cleanText = normalizeCTAText(activeCTA.text || 'Learn More');
            const cleanURL = normalizeCTAURL(activeCTA.url || '');

            if (ctaOverlay) {
                ctaOverlay.classList.remove('has-inline-form');
            }

            if (legacyContent) {
                legacyContent.style.display = '';
            }

            if (ctaForm) {
                ctaForm.style.display = 'none';
                ctaForm.classList.remove('is-gate-layout');
                ctaForm.dataset.ctaType = 'email';

                const nameInput = ctaForm.querySelector('input[name="name"]');
                const emailInput = ctaForm.querySelector('input[name="email"]');
                if (nameInput) {
                    nameInput.style.display = 'block';
                    nameInput.required = true;
                }
                if (emailInput) {
                    emailInput.style.display = 'block';
                    emailInput.required = true;
                }

                const formMessage = ctaForm.querySelector('.video-lead-message');
                if (formMessage) {
                    formMessage.style.display = 'none';
                }
            }

            if (gateForm) {
                gateForm.style.display = 'none';
                gateForm.classList.remove('is-gate-layout');
            }

            if (ctaText) {
                ctaText.textContent = cleanHeroText || 'Ready to take the next step?';
            }

            if (ctaButton) {
                ctaButton.style.display = '';
                ctaButton.textContent = cleanText || 'Learn More';
                if (ctaButton.tagName === 'A') {
                    ctaButton.href = cleanURL || '#';
                    ctaButton.dataset.ctaUrl = cleanURL || '';
                    ctaButton.target = '_blank';
                    ctaButton.rel = 'noopener noreferrer';
                }
            }

            stage.classList.add('show-cta');
            stage.classList.add('hide-cta-play-badge');
            stage.classList.remove('is-gated');
        }

        function renderLegacyGateState(stage, ui = null) {
            if (!stage) return;

            const ctaOverlay = ui?.ctaOverlay || stage.querySelector('.video-cta-overlay');
            const legacyContent = ui?.legacyContent || stage.querySelector('.video-cta-overlay > .video-cta-content');
            const ctaButton = ui?.ctaButton || stage.querySelector('.video-cta-button');
            const ctaForm = ui?.ctaForm || stage.querySelector('.video-cta-form');
            const gateForm = ui?.gateForm || stage.querySelector('.video-gate-form');
            const gateTitle = ui?.gateTitle || stage.querySelector('.gate-form-title');
            const gateSubtext = ui?.gateSubtext || stage.querySelector('.gate-form-subtext');
            const gateSubmit = ui?.gateSubmit || stage.querySelector('.video-gate-submit');
            const video = ui?.video || stage.querySelector('video');

            if (stage.dataset.gateUnlocked === 'true') {
                resetStageCTAUI(stage);
                return;
            }

            stage.classList.add('show-cta');
            stage.classList.add('hide-cta-play-badge');
            stage.classList.add('is-gated');

            if (ctaOverlay) {
                ctaOverlay.classList.remove('has-inline-form');
            }

            if (legacyContent) {
                legacyContent.style.display = 'none';
            }

            if (ctaButton) {
                ctaButton.style.display = 'none';
            }

            if (ctaForm) {
                ctaForm.style.display = 'none';
            }

            if (gateForm) {
                gateForm.style.display = 'block';
                gateForm.dataset.ctaType = 'email_gate';
                gateForm.classList.add('is-gate-layout');
                const formMessage = gateForm.querySelector('.video-lead-message');
                if (formMessage) formMessage.style.display = 'none';
            }

            if (gateTitle) {
                gateTitle.textContent = 'Ready to take the next step?';
            }

            if (gateSubtext) {
                gateSubtext.textContent = 'Enter your name and email below to unlock the full video.';
            }

            if (gateSubmit) {
                gateSubmit.textContent = 'Get Access';
            }

            if (video && !video.paused) {
                video.pause();
            }
        }

        function renderLegacyButtonCTAState(stage, legacyCTAButton, ui = null) {
            if (!stage) return;

            const video = ui?.video || stage.querySelector('video');
            const ctaSeconds = Number(stage.dataset.ctaSeconds || 0);
            if (!video) return;

            if (!ctaSeconds) {
                resetStageCTAUI(stage);
                return;
            }

            const shouldShowCTA = video.currentTime >= ctaSeconds;
            if (shouldShowCTA) {
                if (legacyCTAButton) {
                    const normalizedLegacyURL = normalizeCTAURL(legacyCTAButton.dataset.ctaUrl || legacyCTAButton.getAttribute('href') || '');
                    legacyCTAButton.href = normalizedLegacyURL || '#';
                    legacyCTAButton.target = '_blank';
                    legacyCTAButton.rel = 'noopener noreferrer';
                }

                stage.classList.add('show-cta');
                stage.classList.add('hide-cta-play-badge');
            } else {
                stage.classList.remove('show-cta');
                stage.classList.remove('hide-cta-play-badge');
            }
            stage.classList.remove('is-gated');
        }

        function showLeadFormMessage(messageEl, text, color) {
            if (!messageEl) return;
            messageEl.style.display = 'block';
            messageEl.style.color = color;
            messageEl.textContent = text;
        }

        function clearLeadFormMessage(messageEl) {
            if (!messageEl) return;
            messageEl.style.display = 'none';
            messageEl.style.color = '#9ae6b4';
            messageEl.textContent = 'You’re all set! Enjoy the video.';
        }

        function resetLeadFormFields(fields, submitBtn, formCopy, messageEl, gateForm = null) {
            if (fields) fields.style.display = '';
            if (submitBtn) submitBtn.style.display = '';
            if (formCopy) formCopy.style.marginBottom = '';
            clearLeadFormMessage(messageEl);
            if (gateForm) {
                gateForm.style.display = 'none';
                gateForm.classList.remove('is-gate-layout');
            }
        }

        function handleGateLeadSuccess(stage, form, stageVideo, gateForm) {
            if (!stage || !form || !stageVideo) return;

            stage.dataset.gateUnlocked = 'true';
            const overlay = stage.querySelector('.video-cta-overlay');
            const formUI = getLeadFormUI(form);
            const fields = formUI?.fields;
            const submitBtn = formUI?.submitBtn;
            const formCopy = formUI?.formCopy;
            const formMessage = formUI?.message;

            if (fields) fields.style.display = 'none';
            if (submitBtn) submitBtn.style.display = 'none';
            if (formCopy) formCopy.style.marginBottom = '0';
            showLeadFormMessage(formMessage, 'You’re all set! Enjoy the video.', '#9ae6b4');

            setTimeout(() => {
                if (overlay) overlay.classList.remove('has-inline-form');
                stage.classList.remove('show-cta');
                stage.classList.remove('hide-cta-play-badge');
                stage.classList.remove('is-gated');
                form.style.display = 'none';
                form.classList.remove('is-gate-layout');
                resetLeadFormFields(fields, submitBtn, formCopy, formMessage, gateForm);
                stageVideo.controls = true;
                const originalMuted = stageVideo.muted;
                stageVideo.muted = true;
                stageVideo.play().then(() => {
                    stageVideo.muted = originalMuted;
                }).catch(() => {
                    stageVideo.muted = originalMuted;
                });
            }, 1200);
        }

        function getLeadFormUI(form) {
            if (!form) return null;
            if (form._leadUI) return form._leadUI;

            form._leadUI = {
                emailInput: form.querySelector('input[name="email"]'),
                nameInput: form.querySelector('input[name="name"]'),
                message: form.querySelector('.video-lead-message'),
                fields: form.querySelector('.video-lead-fields'),
                submitBtn: form.querySelector('.video-cta-button'),
                formCopy: form.querySelector('.video-cta-copy'),
                timedFormCopy: form.querySelector('.timed-cta-form-copy')
            };

            return form._leadUI;
        }

        function handleTimedLeadSuccess(stage, form, stageVideo) {
            if (!stage || !form) return;

            const overlay = stage.querySelector('.video-cta-overlay');
            const formUI = getLeadFormUI(form);
            const fields = formUI?.fields;
            const submitBtn = formUI?.submitBtn;
            const formCopy = formUI?.timedFormCopy;
            const message = formUI?.message;
            const currentCTA = getActiveCTA(Math.floor(stageVideo ? stageVideo.currentTime || 0 : 0), stage);

            if (currentCTA && currentCTA.id) {
                setDismissedCTAId(stage, currentCTA.id);
            }

            if (fields) fields.style.display = 'none';
            if (submitBtn) submitBtn.style.display = 'none';
            if (formCopy) formCopy.style.marginBottom = '0';

            setTimeout(() => {
                if (overlay) overlay.classList.remove('has-inline-form');
                stage.classList.remove('show-cta');
                stage.classList.remove('hide-cta-play-badge');
                form.style.display = 'none';
                resetLeadFormFields(fields, submitBtn, formCopy, message);
                if (stageVideo) {
                    stageVideo.play().catch(() => {});
                }
            }, 1200);
        }

        function getCTAEndSecond(index) {
            const currentCTA = timedCTAs[index];
            const nextCTA = timedCTAs[index + 1];
            if (!currentCTA) return Infinity;

            if ((currentCTA.ctaType || 'button') === 'email_gate') {
                return Infinity;
            }

            const defaultDisplaySeconds = 8;
            const gapBeforeNextCTA = 3;
            const naturalEnd = currentCTA.timeSeconds + defaultDisplaySeconds;

            if (!nextCTA) {
                return naturalEnd;
            }

            return Math.max(currentCTA.timeSeconds + 1, Math.min(naturalEnd, nextCTA.timeSeconds - gapBeforeNextCTA));
        }

        function getActiveCTA(currentSecond, stage = null) {
            if (!Array.isArray(timedCTAs) || timedCTAs.length === 0) return null;
            const dismissedCTAIds = getDismissedCTAIds(stage);

            for (let i = 0; i < timedCTAs.length; i++) {
                const cta = timedCTAs[i];
                const startSecond = Number(cta.timeSeconds || 0);
                const endSecond = getCTAEndSecond(i);
                const ctaType = cta.ctaType || 'button';
                if (dismissedCTAIds.has(cta.id)) {
                    continue;
                }

                if (ctaType === 'email_gate') {
                    if (currentSecond >= startSecond) {
                        return cta;
                    }
                    continue;
                }

                if (currentSecond >= startSecond && currentSecond < endSecond) {
                    return cta;
                }
            }

            return null;
        }

        function applyTimedCTA(stage, currentSecond) {
            const ui = stage._ctaUI || {
                ctaOverlay: stage.querySelector('.video-cta-overlay'),
                legacyContent: stage.querySelector('.video-cta-overlay > .video-cta-content'),
                ctaButton: stage.querySelector('.video-cta-button'),
                ctaForm: stage.querySelector('.video-cta-form'),
                ctaSubmit: stage.querySelector('.video-cta-submit'),
                timedTitle: stage.querySelector('.timed-cta-form-title'),
                timedSubtext: stage.querySelector('.timed-cta-form-subtext'),
                gateForm: stage.querySelector('.video-gate-form'),
                gateTitle: stage.querySelector('.gate-form-title'),
                gateSubtext: stage.querySelector('.gate-form-subtext'),
                gateSubmit: stage.querySelector('.video-gate-submit'),
                video: stage.querySelector('video')
            };
            ui.ctaText = ui.legacyContent ? ui.legacyContent.querySelector('.video-cta-text') : null;
            stage._ctaUI = ui;
            const { ctaOverlay } = ui;

            if (!ctaOverlay) return;

            const activeCTA = getActiveCTA(currentSecond, stage);
            if (!activeCTA) {
                resetStageCTAUI(stage, ui);
                return;
            }

            const cleanHeroText = normalizeCTAText(activeCTA.heroText || '');
            const cleanText = normalizeCTAText(activeCTA.text || 'Learn More');
            const cleanURL = normalizeCTAURL(activeCTA.url || '');
            const ctaType = activeCTA.ctaType || 'button';
            const isEmailGateCTA = ctaType === 'email_gate';
            const isEmailCTA = ctaType === 'email' || cleanURL === '';

            if (isEmailGateCTA) {
                renderGateCTA(stage, activeCTA, ui);
                return;
            }

            if (isEmailCTA) {
                renderTimedEmailCTA(stage, activeCTA, ui);
                return;
            }

            renderButtonCTA(stage, activeCTA, ui);
        }

        function sendAnalyticsPing(endpoint, id) {
            if (!id) return;
            const payload = JSON.stringify({ videoId: id });

            if (navigator.sendBeacon) {
                navigator.sendBeacon(`/${endpoint}/${id}`, new Blob([payload], { type: 'application/json' }));
                return;
            }

            fetch(`/${endpoint}/${id}`, {
                method: 'POST', 
                headers: { 'Content-Type': 'application/json' },
                body: payload,
                keepalive: true
            }).catch(() => {});
        }

        function trackVideoDownload() {
            sendAnalyticsPing('download-click', currentVideoId);
        }

        document.querySelectorAll('.video-stage').forEach((stage) => {
            const video = stage.querySelector('video');
            const playBadge = stage.querySelector('.main-play-badge');
            const ctaOverlay = stage.querySelector('.video-cta-overlay');
            const legacyCTAButton = stage.querySelector('.video-cta-overlay a.video-cta-button[data-cta-track="true"]');
            const ctaSeconds = Number(stage.dataset.ctaSeconds || 0);
            const startSeconds = Number(stage.dataset.startSeconds || 0);
            const shouldAutoplay = stage.dataset.autoplay === 'true';
            const shouldMute = stage.dataset.muted === 'true';
            const shouldShowControls = stage.dataset.controls !== 'false';
            const isEmailGate = stage.dataset.ctaType === 'email_gate';
            const isGateUnlocked = () => stage.dataset.gateUnlocked === 'true';
            stage._ctaUI = {
                ctaOverlay,
                legacyContent: stage.querySelector('.video-cta-overlay > .video-cta-content'),
                ctaButton: stage.querySelector('.video-cta-button'),
                ctaForm: stage.querySelector('.video-cta-form'),
                ctaSubmit: stage.querySelector('.video-cta-submit'),
                timedTitle: stage.querySelector('.timed-cta-form-title'),
                timedSubtext: stage.querySelector('.timed-cta-form-subtext'),
                gateForm: stage.querySelector('.video-gate-form'),
                gateTitle: stage.querySelector('.gate-form-title'),
                gateSubtext: stage.querySelector('.gate-form-subtext'),
                gateSubmit: stage.querySelector('.video-gate-submit'),
                video
            };
            stage._ctaUI.ctaText = stage._ctaUI.legacyContent ? stage._ctaUI.legacyContent.querySelector('.video-cta-text') : null;
            if (!video) return;

            video.muted = shouldMute;
            video.controls = shouldShowControls;

            const applyStartTime = () => {
                if (startSeconds > 0 && !Number.isNaN(startSeconds)) {
                    try {
                        video.currentTime = startSeconds;
                    } catch (error) {
                        // Ignore seek timing issues until metadata is ready
                    }
                }
            };

            let retentionInterval = null;

            const startRetentionTracking = () => {
                if (retentionInterval) return;
                retentionInterval = setInterval(() => {
                    if (video.paused || video.ended) return;
                    const currentSecond = Math.floor(video.currentTime);
                    trackRetention(currentSecond);
                }, 3000);
            };

            const stopRetentionTracking = () => {
                if (retentionInterval) {
                    clearInterval(retentionInterval);
                    retentionInterval = null;
                }
            };

            video.addEventListener('loadedmetadata', () => {
                applyStartTime();
                trackRetention(Math.floor(video.currentTime));
            }, { once: true });

            const syncPlayState = () => {
                if (video.paused || video.ended) {
                    stage.classList.remove('is-playing');
                } else {
                    stage.classList.add('is-playing');
                }
            };

            const syncCTAState = () => {
                if (!ctaOverlay || video.ended) {
                    resetStageCTAUI(stage, stage._ctaUI);
                    return;
                }

                if (isEmailGate) {
                    renderLegacyGateState(stage, stage._ctaUI);
                    return;
                }

                if (Array.isArray(timedCTAs) && timedCTAs.length > 0) {
                    applyTimedCTA(stage, Math.floor(video.currentTime || 0));
                    return;
                }

                renderLegacyButtonCTAState(stage, legacyCTAButton, stage._ctaUI);
            };

            if (playBadge) {
                playBadge.addEventListener('click', () => {
                    if (video.paused || video.ended) {
                        video.play();
                    } else {
                        video.pause();
                    }
                });
            }

            video.addEventListener('play', () => {
                syncPlayState();
                syncCTAState();
                startRetentionTracking();
            });
            video.addEventListener('pause', () => {
                syncPlayState();
                syncCTAState();
                stopRetentionTracking();
            });
            video.addEventListener('ended', () => {
                syncPlayState();
                syncCTAState();
                stopRetentionTracking();
                trackRetention(Math.floor(video.duration || video.currentTime || 0));
            });
            video.addEventListener('timeupdate', syncCTAState);
            video.addEventListener('seeked', syncCTAState);

            if (isEmailGate && !isGateUnlocked()) {
                video.pause();
                stopRetentionTracking();
            } else if (shouldAutoplay) {
                video.play().catch(() => {});
            }

            syncPlayState();
            syncCTAState();
        });

        document.querySelectorAll('.video-lead-form').forEach((form) => {
            form.addEventListener('submit', async (event) => {
                event.preventDefault();
                const videoId = form.dataset.videoId;
                const formUI = getLeadFormUI(form);
                const emailInput = formUI?.emailInput;
                const nameInput = formUI?.nameInput;
                const message = formUI?.message;
                const stage = form.closest('.video-stage');
                const stageVideo = stage ? stage.querySelector('video') : null;
                const isEmailGateForm = form.dataset.ctaType === 'email_gate';
                const gateForm = stage ? stage.querySelector('.video-gate-form') : null;
                if (!videoId || !emailInput || !emailInput.value.trim()) return;
                if (isEmailGateForm && (!nameInput || !nameInput.value.trim())) return;

                const body = new URLSearchParams({
                    email: emailInput.value.trim(),
                    name: nameInput ? nameInput.value.trim() : ''
                });
                try {
                    const response = await fetch(`/capture-lead/${videoId}`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                        body: body.toString()
                    });
                    if (!response.ok) throw new Error('failed');

                    emailInput.value = '';
                    if (nameInput) {
                        nameInput.value = '';
                    }

                    showLeadFormMessage(message, 'You’re all set! Enjoy the video.', '#9ae6b4');

                    if (isEmailGateForm && stage && stageVideo) {
                        handleGateLeadSuccess(stage, form, stageVideo, gateForm);
                    } else if (stage) {
                        handleTimedLeadSuccess(stage, form, stageVideo);
                    }
                } catch (error) {
                    showLeadFormMessage(message, 'Unable to submit right now. Please try again.', '#fca5a5');
                }
            });
        });
        document.querySelectorAll('[data-cta-track="true"]').forEach((link) => {
            link.addEventListener('click', (event) => {
                const videoId = link.dataset.videoId;
                if (!videoId) return;

                const normalizedURL = normalizeCTAURL(link.dataset.ctaUrl || link.getAttribute('href') || '');
                if (!normalizedURL) {
                    event.preventDefault();
                    return;
                }

                link.href = normalizedURL;
                link.target = '_blank';
                link.rel = 'noopener noreferrer';

                const payload = JSON.stringify({ videoId });

                if (navigator.sendBeacon) {
                    navigator.sendBeacon(`/cta-click/${videoId}`, new Blob([payload], { type: 'application/json' }));
                    return;
                }

                fetch(`/cta-click/${videoId}`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: payload,
                    keepalive: true
                }).catch(() => {});
            });
        });