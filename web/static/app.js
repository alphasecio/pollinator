// True once, for the lifetime of this page load — app.js is identical on
// every page, and this is the one thing that should only ever happen on
// the projected screen, not on every participant's phone.
const isDisplayPage = window.location.pathname === "/display";

// --- countdown tick sound (display only) ---
//
// Synthesized, not a licensed track — see the standalone demo this was
// prototyped from. Gentle low tick through most of the countdown, a
// quicker higher-pitched tick for the last 5 seconds, a low, long-decaying
// gong at zero. No mute control by design — the host's own laptop volume (or
// muting the browser tab) already does that job, and duplicating it in
// the app is one more thing to build and get wrong for no real gain.
let audioCtx = null;

function getAudioCtx() {
    if (!audioCtx) audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    return audioCtx;
}

function playTone(freq, duration, type, volume) {
    const ctx = getAudioCtx();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = type;
    osc.frequency.value = freq;
    gain.connect(ctx.destination);
    osc.connect(gain);
    const now = ctx.currentTime;
    gain.gain.setValueAtTime(volume, now);
    gain.gain.exponentialRampToValueAtTime(0.001, now + duration);
    osc.start(now);
    osc.stop(now + duration);
}

function playCountdownTick(secondsLeft) {
    if (secondsLeft > 5) {
        playTone(600, 0.07, "sine", 0.1);
    } else if (secondsLeft > 0) {
        playTone(880, 0.09, "square", 0.15);
    }
}

// A real gong's character comes from inharmonic partials (not clean
// integer multiples of a fundamental, unlike a musical chord) each with
// a slow decay — a few seconds, not the fraction-of-a-second ticks above.
// That combination is what actually reads as "struck metal" rather than
// just a longer beep.
function playCountdownGong() {
    playTone(180, 2.2, "sine", 0.14);
    playTone(302, 1.8, "sine", 0.09);
    playTone(417, 1.4, "sine", 0.06);
    playTone(588, 1.0, "sine", 0.04);
}

// Most browsers refuse to make sound at all until the page has seen some
// real user interaction — simply loading /display doesn't count. This is
// a silent, invisible unlock (not a control the host has to notice or
// use), armed once and torn down after it fires. If the host never clicks
// anywhere on the tab before the very first question starts, that
// question's ticks may be silent; every question after the first click
// won't be.
function armAudioUnlock() {
    if (!isDisplayPage) return;
    const unlock = () => {
        getAudioCtx();
        document.removeEventListener("click", unlock);
    };
    document.addEventListener("click", unlock);
}

// last{Second,End} persist at module scope, not per-element, on purpose:
// #app gets replaced wholesale on every unrelated broadcast (someone
// answering, most of all — which happens constantly during exactly the
// phase this sound plays in), and each replacement is a brand new DOM
// node with no memory of what the previous node already played. Tracking
// "which second we've already played a tick for" here instead means a
// remount triggered by someone else's action can't replay the same
// second's tick — only a real second actually elapsing does, and a
// genuinely new question (different end timestamp) correctly resets it.
let lastTickSecond = null;
let lastTickEnd = null;

function maybePlayCountdownSound(end, secondsLeft) {
    if (!isDisplayPage) return;

    if (end !== lastTickEnd) {
        lastTickEnd = end;
        lastTickSecond = null;
    }
    if (secondsLeft === lastTickSecond) return;
    lastTickSecond = secondsLeft;

    if (secondsLeft > 0) {
        playCountdownTick(secondsLeft);
    } else {
        playCountdownGong();
    }
}

// The server owns the actual deadline (see internal/hub.go's armTimer) and
// closes the question regardless of what any client shows. This is purely
// a courtesy tick so the number visibly counts down between broadcasts,
// computed once from the end timestamp the server sent — not a per-second
// message from the server.
//
// One shared interval, not one per element: #app can be replaced many
// times over a single countdown's lifetime (every answer submission
// remounts it), and a per-element reference can never find the *previous*
// element's interval to clear it — that element is already gone. Tracking
// the interval here instead means every call to this function correctly
// tears down whatever was running before starting fresh, with nothing
// orphaned left ticking in the background.
let countdownIntervalId = null;

// countdownDeadline/countdownEndKey persist at module scope, alongside
// lastTickSecond/lastTickEnd above, for the same reason: #app can remount
// many times over one question's lifetime, and re-deriving the deadline on
// every remount is both unnecessary and, if it ever used Date.now() as
// anything other than a one-time anchor, would reintroduce exactly the
// clock-comparison problem being fixed here.
let countdownDeadline = null;
let countdownEndKey = null;

function tickCountdowns() {
    if (countdownIntervalId) {
        clearInterval(countdownIntervalId);
        countdownIntervalId = null;
    }

    const el = document.querySelector("[data-countdown-end]");
    if (!el) return;

    // 'end' (the server's absolute deadline) is used only as a stable
    // identity — to detect "is this the same question as last render" —
    // never compared against the client's own clock for timing. Comparing
    // an absolute server timestamp against the client's Date.now() is
    // exactly what breaks if the two clocks disagree, even by a few
    // seconds, which is common enough on real devices to matter.
    //
    // Timing instead comes from the server's already-relative SecondsLeft:
    // anchoring "SecondsLeft more seconds from right now" only needs the
    // client's own clock to progress normally, which is true even if its
    // absolute reading is wrong — there's no cross-clock comparison at all
    // after this one-time anchor.
    const end = parseInt(el.dataset.countdownEnd, 10);
    if (end !== countdownEndKey) {
        countdownEndKey = end;
        const serverSecondsLeft = parseInt(el.dataset.secondsLeft, 10);
        countdownDeadline = Date.now() + serverSecondsLeft * 1000;
    }

    const update = () => {
        const current = document.querySelector("[data-countdown-end]");
        if (!current) {
            clearInterval(countdownIntervalId);
            countdownIntervalId = null;
            return;
        }
        const secondsLeft = Math.max(0, Math.round((countdownDeadline - Date.now()) / 1000));
        current.textContent = secondsLeft;
        maybePlayCountdownSound(end, secondsLeft);

        if (secondsLeft <= 0) {
            clearInterval(countdownIntervalId);
            countdownIntervalId = null;
        }
    };
    update();
    countdownIntervalId = setInterval(update, 1000);
}

// The questions drawer's open/closed state lives here, in a plain JS
// variable, rather than in a CSS class on the drawer element itself. That's
// deliberate: admin's #app gets replaced wholesale on every SSE broadcast
// (someone joins, answers, anything) — which happens often while the host
// might have this open — so anything storing "is it open" only in the DOM
// would get silently reset the moment an unrelated participant did
// something. Keeping the state here and re-applying it after every swap
// means the drawer stays open across broadcasts until the host actually
// closes it.
let questionsDrawerOpen = false;

function applyQuestionsDrawerState() {
    const drawer = document.querySelector("[data-questions-drawer]");
    const backdrop = document.querySelector("[data-questions-backdrop]");
    if (!drawer || !backdrop) return;

    drawer.classList.toggle("translate-x-full", !questionsDrawerOpen);
    backdrop.classList.toggle("opacity-0", !questionsDrawerOpen);
    backdrop.classList.toggle("pointer-events-none", !questionsDrawerOpen);
}

function initQuestionsDrawer() {
    document.querySelectorAll("[data-questions-toggle]").forEach((btn) => {
        btn.addEventListener("click", () => {
            questionsDrawerOpen = !questionsDrawerOpen;
            applyQuestionsDrawerState();
        });
    });
    document.querySelectorAll("[data-questions-backdrop]").forEach((el) => {
        el.addEventListener("click", () => {
            questionsDrawerOpen = false;
            applyQuestionsDrawerState();
        });
    });
    applyQuestionsDrawerState();
}

// --- poll setup/edit form ---
//
// editingPoll blocks incoming SSE swaps entirely (see the htmx:beforeSwap
// listener below) while true. This matters for a real reason, not just
// caution: the edit form's question/option cards are built and held
// client-side in the DOM, not server-rendered — if an unrelated broadcast
// (someone joining, say) swapped #app out from under the host mid-edit,
// every card they'd added or typed into would be silently destroyed with
// no way to recover it. Blocking swaps while editing trades a few seconds
// of stale "N joined" text for never losing in-progress edits, which is
// obviously the right side of that trade for a form nobody fills out more
// than once per poll anyway.
let editingPoll = false;
const MAX_POLL_OPTIONS = 4;
let pollUid = 0;

function findPollCard(id) {
    return document.querySelector(`#poll-questions > [data-id="${id}"]`);
}

function addPollQuestion(questionText, options) {
    pollUid++;
    const id = pollUid;

    const card = document.createElement("div");
    card.className = "rounded-xl bg-zinc-900 border border-zinc-800 p-4 mb-3";
    card.dataset.id = String(id);
    card.innerHTML = `
        <div class="flex items-center justify-between mb-2">
            <span class="text-xs text-zinc-500 uppercase tracking-wide pq-label"></span>
            <button type="button" class="text-zinc-500 hover:text-red-400 text-lg leading-none" onclick="removePollQuestion(${id})">&times;</button>
        </div>
        <input type="text" class="pq-text w-full rounded-lg bg-zinc-950 border border-zinc-800 px-3 py-2 mb-2
                                   focus:outline-none focus:ring-2 focus:ring-pollen focus:border-transparent transition"
               placeholder="Question text" maxlength="200">
        <div class="flex items-center justify-between mb-1">
            <span class="text-xs text-zinc-500">Options (up to ${MAX_POLL_OPTIONS})</span>
            <button type="button" class="text-xs text-zinc-500 hover:text-pollen pq-add-option" onclick="addPollOption(${id})">+ Add option</button>
        </div>
        <div class="pq-options space-y-2"></div>
    `;
    document.getElementById("poll-questions").appendChild(card);
    card.querySelector(".pq-text").value = questionText || "";

    const opts = (options && options.length) ? options : ["", ""];
    opts.forEach((o) => addPollOption(id, o));

    renumberPollQuestions();
}

function addPollOption(questionId, value) {
    const card = findPollCard(questionId);
    const container = card.querySelector(".pq-options");
    if (container.children.length >= MAX_POLL_OPTIONS) return;

    const row = document.createElement("div");
    row.className = "flex gap-2";
    row.innerHTML = `
        <input type="text" class="pq-option flex-1 rounded-lg bg-zinc-950 border border-zinc-800 px-3 py-2
                                    focus:outline-none focus:ring-2 focus:ring-pollen focus:border-transparent transition"
               placeholder="Option" maxlength="60">
        <button type="button" class="text-zinc-500 hover:text-red-400 text-lg leading-none" onclick="removePollOption(${questionId}, this)">&times;</button>
    `;
    container.appendChild(row);
    row.querySelector(".pq-option").value = value || "";

    updatePollAddOptionState(card);
}

function removePollOption(questionId, btn) {
    const card = findPollCard(questionId);
    btn.parentElement.remove();
    updatePollAddOptionState(card);
}

function updatePollAddOptionState(card) {
    const count = card.querySelectorAll(".pq-option").length;
    card.querySelector(".pq-add-option").classList.toggle("hidden", count >= MAX_POLL_OPTIONS);
}

function removePollQuestion(id) {
    const card = findPollCard(id);
    if (card) card.remove();
    renumberPollQuestions();
}

// Visible "Question N" labels always reflect current position in the
// list, recomputed after every add/remove — never a permanent counter,
// which drifts from this the moment anything is deleted (learned that one
// the hard way in the standalone poll-builder tool — same bug, fixed here
// from the start instead).
function renumberPollQuestions() {
    document.querySelectorAll("#poll-questions > [data-id]").forEach((card, i) => {
        card.querySelector(".pq-label").textContent = `Question ${i + 1}`;
    });
}

function loadPollSeed() {
    const seedEl = document.getElementById("poll-seed");
    let seed = null;
    if (seedEl && seedEl.dataset.seed) {
        try {
            // atob() decodes base64 into a string where each character is
            // one raw byte (Latin-1 style) — not proper UTF-8. Any emoji or
            // non-Latin text in the poll would otherwise corrupt into
            // mojibake or throw entirely. TextDecoder reassembles those
            // raw bytes into the real UTF-8 string first.
            const raw = atob(seedEl.dataset.seed);
            const bytes = Uint8Array.from(raw, (c) => c.charCodeAt(0));
            seed = JSON.parse(new TextDecoder().decode(bytes));
        } catch (e) {
            seed = null;
        }
    }

    const titleEl = document.getElementById("poll-title");
    const durationEl = document.getElementById("poll-duration");

    if (seed) {
        titleEl.value = seed.title || "";
        durationEl.value = seed.duration || 20;
        (seed.questions || []).forEach((q) => addPollQuestion(q.question, q.options));
    } else {
        durationEl.value = 20;
        addPollQuestion();
    }
}

// The dynamic question cards are built once from seed data, not rebuilt on
// every render — data-initialized guards against doing it twice if init()
// runs again for an unrelated reason (e.g. tickCountdowns needing a
// re-arm) while the form is already populated.
function initPollFormIfNeeded() {
    const container = document.getElementById("poll-questions");
    if (!container || container.dataset.initialized === "true") return;
    container.dataset.initialized = "true";
    loadPollSeed();
}

function initPollEditor() {
    // #poll-questions only exists on admin's page at all — on join/display
    // it's simply not there, and this must no-op rather than fall through
    // to "no #admin-ready found" below, which used to wrongly conclude
    // "nothing configured, suppress swaps" on every page, not just admin.
    // That's exactly what broke the join-name-collision error message: it
    // silenced htmx:beforeSwap globally, including the join form's own
    // error swap, on a page that has nothing to do with poll editing at all.
    if (!document.getElementById("poll-questions")) return;

    const readyView = document.getElementById("admin-ready");
    if (!readyView) {
        // No poll configured yet — the form is the only thing on screen,
        // so it's effectively always "being edited" from the start.
        editingPoll = true;
    }
    initPollFormIfNeeded();
}

function showEditPoll() {
    document.getElementById("admin-ready").classList.add("hidden");
    document.getElementById("admin-edit").classList.remove("hidden");
    editingPoll = true;
    initPollFormIfNeeded();
}

function cancelEditPoll() {
    window.location.reload();
}

// gatherPollData reads the form exactly as it currently stands — used by
// both savePoll and exportPollJSON, so there's one place validation
// rules live rather than two copies that could quietly diverge (the
// standalone builder tool already taught us that lesson once).
function gatherPollData() {
    const title = document.getElementById("poll-title").value.trim();
    const duration = parseInt(document.getElementById("poll-duration").value.trim(), 10);

    const cards = document.querySelectorAll("#poll-questions > [data-id]");
    if (cards.length === 0) {
        return { error: "Add at least one question." };
    }

    const questions = [];
    let error = null;

    cards.forEach((card, i) => {
        if (error) return;

        const qText = card.querySelector(".pq-text").value.trim();
        const rawOptions = Array.from(card.querySelectorAll(".pq-option")).map((el) => el.value.trim());

        if (!qText) {
            error = `Question ${i + 1} is missing its text.`;
            return;
        }
        if (rawOptions.some((v) => v === "")) {
            error = `Question ${i + 1} has an empty option — fill it in or remove it.`;
            return;
        }
        if (rawOptions.length < 2) {
            error = `Question ${i + 1} needs at least two options.`;
            return;
        }

        questions.push({ question: qText, options: rawOptions });
    });

    if (error) return { error };

    return {
        data: {
            title: title,
            duration: duration > 0 ? duration : 20,
            questions: questions,
        },
    };
}

// exportPollJSON downloads whatever is currently in the form — including
// unsaved edits — not just whatever was last saved. Deliberately more
// useful than exporting only the saved poll: a redeploy wipes anything
// that only ever lived in memory, so this is the offline copy that
// protects against losing work before you've even clicked Save.
function exportPollJSON() {
    const errorEl = document.getElementById("poll-error");
    errorEl.innerHTML = "";

    const result = gatherPollData();
    if (result.error) {
        errorEl.innerHTML = `<p class="text-sm text-red-400 mt-2">${result.error}</p>`;
        return;
    }

    const json = JSON.stringify(result.data, null, 2);
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);

    const a = document.createElement("a");
    a.href = url;
    a.download = "poll.json";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

// importPollJSON reads a previously-exported (or hand-written) poll.json
// and replaces whatever's currently in the form with it. Reuses
// addPollQuestion/addPollOption exactly as loadPollSeed already does for
// the server-rendered seed — importing is really just seeding the form
// from a different source, not a separate mechanism.
function importPollJSON(inputEl) {
    const file = inputEl.files[0];
    if (!file) return;

    const errorEl = document.getElementById("poll-error");
    errorEl.innerHTML = "";

    const reader = new FileReader();
    reader.onload = () => {
        let data;
        try {
            data = JSON.parse(reader.result);
        } catch (e) {
            errorEl.innerHTML = '<p class="text-sm text-red-400 mt-2">That file isn\'t valid JSON.</p>';
            inputEl.value = "";
            return;
        }

        if (!data || !Array.isArray(data.questions) || data.questions.length === 0) {
            errorEl.innerHTML = '<p class="text-sm text-red-400 mt-2">That file doesn\'t look like a poll — expected a "questions" array.</p>';
            inputEl.value = "";
            return;
        }

        if (!window.confirm("Replace what's in this form with the imported file?")) {
            inputEl.value = "";
            return;
        }

        document.getElementById("poll-questions").innerHTML = "";
        document.getElementById("poll-title").value = data.title || "";
        document.getElementById("poll-duration").value = data.duration || 20;
        data.questions.forEach((q) => addPollQuestion(q.question, q.options));

        // Reset so importing the same file again still fires onchange.
        inputEl.value = "";
    };
    reader.readAsText(file);
}

async function savePoll(adminBase) {
    const errorEl = document.getElementById("poll-error");
    errorEl.innerHTML = "";

    const result = gatherPollData();
    if (result.error) {
        errorEl.innerHTML = `<p class="text-sm text-red-400 mt-2">${result.error}</p>`;
        return;
    }

    try {
        const res = await fetch(adminBase + "/poll", {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: new URLSearchParams({ poll_json: JSON.stringify(result.data) }),
        });

        if (res.ok) {
            editingPoll = false;
            window.location.reload();
        } else {
            errorEl.innerHTML = await res.text();
        }
    } catch (e) {
        errorEl.innerHTML = '<p class="text-sm text-red-400 mt-2">Could not reach the server — try again.</p>';
    }
}

function init() {
    tickCountdowns();
    initQuestionsDrawer();
    initPollEditor();
}

armAudioUnlock();

document.addEventListener("DOMContentLoaded", init);
// Every SSE push replaces the fragment wholesale (see base.html's
// sse-swap="message"), so anything bound to elements inside it needs to be
// re-initialized against whatever new DOM just arrived.
document.body.addEventListener("htmx:afterSwap", init);

// See the comment on editingPoll above — this is what actually enforces
// the "don't destroy in-progress edits" guarantee. htmx dispatches this
// before every swap, including SSE-driven ones, and setting shouldSwap to
// false cancels it outright.
document.body.addEventListener("htmx:beforeSwap", (evt) => {
    if (editingPoll) {
        evt.detail.shouldSwap = false;
    }
});
