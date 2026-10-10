port module TestRunner exposing (main)

import AccessTests
import CollectionLoadMoreTests
import CollectionRefreshTests
import Json.Encode as Encode
import Platform
import RevisionTests
import UploadQueueTests
import WorkspaceControlTests


port run : (() -> msg) -> Sub msg


port report : Encode.Value -> Cmd msg


type Msg
    = Run


allTests : List ( String, Bool )
allTests =
    RevisionTests.suite ++ UploadQueueTests.suite ++ AccessTests.suite ++ CollectionRefreshTests.suite ++ CollectionLoadMoreTests.suite ++ WorkspaceControlTests.suite


encodeTest : ( String, Bool ) -> Encode.Value
encodeTest ( name, passed ) =
    Encode.object
        [ ( "name", Encode.string name )
        , ( "passed", Encode.bool passed )
        ]


main : Program () () Msg
main =
    Platform.worker
        { init = \() -> ( (), Cmd.none )
        , update =
            \_ model ->
                ( model
                , report (Encode.list encodeTest allTests)
                )
        , subscriptions = \_ -> run (\_ -> Run)
        }
