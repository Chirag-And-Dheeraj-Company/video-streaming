window.onload = async () => {
  const response = await fetch(`${window.ENV.API_URL}/video`, {
    method: "GET",
  });

  let videos = await response.json();


  if (videos.length > 0) {
    let videoListSection = document.getElementById("video_list");

    for (let i = 0; i < videos.length; i++) {
      let videoTitleText = document.createTextNode(videos[i].title);
      let videoDescriptionText = document.createTextNode(videos[i].description);
      let videoListRow = document.createElement("section");
      let videoTitle = document.createElement("h2");
      let videoDescription = document.createElement("p");
      let videoLink = document.createElement("a");
      let deleteLink = document.createElement("a");
      let videoThumbnail = document.createElement("img");

      videoThumbnail.src = "http://127.0.0.1:8000/static/logo/dekho_logo.svg";

      videoLink.setAttribute(
        "href",
        `${window.ENV.API_URL}/watch?v=${videos[i].id}`
      );
      videoLink.textContent = "Play";

      deleteLink.setAttribute("href", "/list");
      deleteLink.setAttribute("data-id", videos[i].id);
      deleteLink.textContent = "Delete";

      videoTitle.appendChild(videoTitleText);

      videoDescription.appendChild(videoDescriptionText);

      videoListRow.classList.add("hunter");

      videoListRow.appendChild(videoThumbnail);
        
      videoListRow.appendChild(videoTitle);
      videoListRow.appendChild(videoDescription);
      videoListRow.appendChild(videoLink);
      videoListRow.appendChild(deleteLink);

      videoListSection.appendChild(videoListRow);

      deleteLink.addEventListener("click", async (e) => {
        e.preventDefault();
        console.log("Delete Video");

        const video_id = deleteLink.dataset.id;
        console.log(video_id);
        deleteLink.textContent = "Deleting...";

        const response = await fetch(
          `${window.ENV.API_URL}/video/${video_id}`,
          {
            method: "DELETE",
          }
        );

        if (response.status === 202) {
          deleteLink.textContent = "Deleted";
        } else {
          deleteLink.textContent = "Error while deleting";
        }

        location.reload();
      });
    }
  }
};
