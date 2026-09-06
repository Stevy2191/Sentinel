// Redesign commits to a fixed dark theme. Always dark, before first paint.
// Kept as an external file (rather than inline in index.html) so the CSP's
// script-src can be 'self' only, with no 'unsafe-inline' needed.
document.documentElement.classList.add('dark')
