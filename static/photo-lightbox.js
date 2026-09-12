(() => {
  const links = document.querySelectorAll("[data-photo-lightbox]");

  links.forEach((link) => {
    link.addEventListener("click", (event) => {
      if (!window.HTMLDialogElement) {
        return;
      }

      event.preventDefault();

      const dialog = document.createElement("dialog");
      dialog.className = "photo-lightbox";

      const image = document.createElement("img");
      image.src = link.href;
      image.alt = link.querySelector("img")?.alt || "";

      const closeButton = document.createElement("button");
      closeButton.className = "photo-lightbox-close";
      closeButton.type = "button";
      closeButton.setAttribute("aria-label", link.dataset.closeLabel || "Close");
      closeButton.textContent = "×";

      closeButton.addEventListener("click", () => dialog.close());
      dialog.addEventListener("click", (dialogEvent) => {
        if (dialogEvent.target === dialog) {
          dialog.close();
        }
      });
      dialog.addEventListener("close", () => dialog.remove());

      dialog.append(image, closeButton);
      document.body.append(dialog);
      dialog.showModal();
    });
  });
})();
