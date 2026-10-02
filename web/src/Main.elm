module Main exposing (main)

import Browser
import Json.Decode as Decode
import Page.Library as Library


main : Program Decode.Value Library.Model Library.Msg
main =
    Browser.application
        { init = Library.init
        , onUrlChange = Library.onUrlChange
        , onUrlRequest = Library.onUrlRequest
        , subscriptions = Library.subscriptions
        , update = Library.update
        , view = Library.view
        }
