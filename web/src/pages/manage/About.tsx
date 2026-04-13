import { Center, Text, VStack } from "@hope-ui/solid"
import { useManageTitle } from "~/hooks"
import { getSetting } from "~/store"

const About = () => {
  useManageTitle("manage.sidemenu.about")
  return (
    <Center h="$full">
      <VStack spacing="$2">
        <Text fontSize="$xl" fontWeight="$semibold">
          {getSetting("site_title") || "AList"}
        </Text>
        <Text color="$neutral11">Version {getSetting("version") || "Unknown"}</Text>
      </VStack>
    </Center>
  )
}

export default About
