import { useManageTitle } from "~/hooks"
import { TypeTasks } from "./Tasks"

const BaiduUpload = () => {
  useManageTitle("manage.sidemenu.baidu_upload")
  return <TypeTasks type="baidu_upload" canRetry />
}

export default BaiduUpload
