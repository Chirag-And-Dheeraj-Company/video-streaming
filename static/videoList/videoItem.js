class VideoItem extends HTMLElement {
  constructor() {
    super();
    this.shadow = this.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = `
            :host {
                display: block; /* Make the host element visible */
            }
            .video-item {
                display: flex;
                align-items: flex-start;
                padding: 1rem;
                border-radius: 8px;
                background-color: #f5f5f5;
                color: black;
                margin-bottom: 1rem;
                width: 100%;
            }
            .thumbnail {
                width: 150px;
                height: auto;
                object-fit: cover;
                margin-right: 1rem;
                border-radius: 4px;
            }
            .content {
                flex-grow: 1;
            }
            .actions {
                display: flex;
                gap: 0.5rem;
                align-items: center;
            }
            .action-button {
                padding: 0.25rem 0.75rem;
                border-radius: 4px;
                text-decoration: none;
                color: white;
                font-weight: 500;
            }
            .play {
                background-color: #a0a0ff;
            }
            .delete {
                background-color: #ff4444;
            }
        `;

    const template = document.createElement("template");

    template.innerHTML = `
        <div class="video-item">
            <img src="../static/logo/android-chrome-192x192.png" alt="lol" class="thumbnail" />
            <div class="content">
                <h3 class="name"></h3>
                <p class="description"></p>
            </div>
            <div class="actions">
                <a href="" class="action-button play">Play</a>
                <a href="" class="action-button delete">Delete</a>
            </div>
        </div>
    `;

    this.shadow.appendChild(style);

    this.shadow.appendChild(template.content.cloneNode(true));

    this.initialize();
  }

  initialize() {
    // Set up event delegation
    this.shadowRoot.addEventListener("click", (e) => {
      const target = e.target;

      if (target.classList.contains("play")) {
        e.preventDefault();
        this.handlePlay();
      } else if (target.classList.contains("delete")) {
        e.preventDefault();
        this.handleDelete();
      }
    });
  }

  handlePlay() {
    const videoId = this.getAttribute("video-id");
    const playButton = this.shadowRoot.querySelector(".play");
    window.location.href = `${window.ENV.API_URL}/watch?v=${videoId}`;
  }

  handleDelete() {
    const videoId = this.getAttribute("video-id");
    const deleteButton = this.shadowRoot.querySelector(".delete");

    deleteButton.textContent = "Deleting...";

    fetch(`${window.ENV.API_URL}/video/${videoId}`, {
      method: "DELETE",
    })
      .then((response) => {
        if (response.ok) {
          this.remove();
        } else {
          deleteButton.textContent = "Error";
        }
      })
      .catch((error) => {
        deleteButton.textContent = "Error";
        console.error("Error deleting video:", error);
      });
  }

  static get observedAttributes() {
    return ["name", "description", "thumbnail", "video-id"];
  }

  attributeChangedCallback(name, oldValue, newValue) {
    const element = this.shadow.querySelector(`.${name}`);
    if (element) {
      if (name === "thumbnail") {
        // element.src = newValue;
      } else if (name === "name") {
        element.textContent = newValue;
      } else if (name === "description") {
        element.textContent = newValue;
      }
    }
  }

  disconnectedCallback() {
    // Clean up the single event listener
    this.shadowRoot.removeEventListener(
      "click",
      this.shadowRoot.lastEventCallback
    );
  }
}

customElements.define("video-item", VideoItem);
