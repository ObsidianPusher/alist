import { useManageTitle } from "~/hooks"
import { TypeTasks } from "./Tasks"

const BaiduDownload = () => {
  useManageTitle("manage.sidemenu.baidu_download")
  return <TypeTasks type="baidu_download" canRetry />
}

export default BaiduDownload
