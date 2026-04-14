import axios from "axios"

export const fetchTextContent = async (
  url: string,
  ts = true,
): Promise<{
  content: ArrayBuffer | string
  contentType?: string
}> => {
  try {
    const resp = await axios.get(url, {
      responseType: "blob",
      params: ts
        ? {
            alist_ts: new Date().getTime(),
          }
        : undefined,
    })
    const content = await resp.data.arrayBuffer()
    const contentType = resp.headers["content-type"]
    return { content, contentType }
  } catch (e) {
    return ts
      ? await fetchTextContent(url, false)
      : {
          content: `Failed to fetch ${url}: ${e}`,
          contentType: "",
        }
  }
}
