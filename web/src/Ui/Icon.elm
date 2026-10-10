module Ui.Icon exposing (view)

import Html exposing (Html, span)
import Html.Attributes exposing (attribute, class)


view : String -> Html msg
view name =
    span [ class ("icon icon-" ++ name), attribute "aria-hidden" "true" ] []
