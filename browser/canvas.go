package browser

import "github.com/go-rod/rod"

func snapshotHTML(page *rod.Page) (string, error) {
	if err := snapshotCanvasPixels(page); err != nil {
		return "", err
	}
	return page.HTML()
}

func snapshotCanvasPixels(page *rod.Page) error {
	_, err := page.Eval(`() => {
		const maxCanvasSnapshotPixels = 4096 * 4096;
		for (const canvas of document.querySelectorAll("canvas")) {
			try {
				if (canvas.width === 0 || canvas.height === 0) continue;
				if (canvas.width * canvas.height > maxCanvasSnapshotPixels) continue;
				const src = canvas.toDataURL("image/png");
				if (!src.startsWith("data:image/png")) continue;
				const img = document.createElement("img");
				for (const attr of canvas.attributes) img.setAttribute(attr.name, attr.value);
				img.setAttribute("src", src);
				if (!img.hasAttribute("alt")) img.setAttribute("alt", "");
				if (!img.hasAttribute("width")) img.setAttribute("width", String(canvas.width));
				if (!img.hasAttribute("height")) img.setAttribute("height", String(canvas.height));
				const rect = canvas.getBoundingClientRect();
				if (!img.style.width && rect.width > 0) img.style.width = rect.width + "px";
				if (!img.style.height && rect.height > 0) img.style.height = rect.height + "px";
				canvas.replaceWith(img);
			} catch (_) {}
		}
	}`)
	return err
}
