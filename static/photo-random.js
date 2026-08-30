(() => {
  const links = document.querySelectorAll("[data-photo-destinations]");

  links.forEach((link) => {
    let destinations;

    try {
      destinations = JSON.parse(link.dataset.photoDestinations);
    } catch {
      return;
    }

    if (!Array.isArray(destinations) || destinations.length === 0) {
      return;
    }

    link.addEventListener("click", () => {
      const index = Math.floor(Math.random() * destinations.length);
      link.href = destinations[index];
    });
  });
})();
