module Ui.Kbd exposing (view)

import Html exposing (Html, kbd, text)
import Html.Attributes exposing (class)


view : String -> Html msg
view key =
    kbd [ class "kbd" ] [ text key ]
