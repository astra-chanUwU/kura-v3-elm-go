port module TestRunner exposing (main)

import AccessTests
import ApiErrorTests
import CollectionLoadMoreTests
import CollectionRefreshTests
import Json.Encode as Encode
import Platform
import CollectionsTests
import RevisionTests
import ReactionsTests
import SearchTests
import TagsTests
import QueueTests
import UploadQueueTests
import WorkspaceControlTests


port run : (() -> msg) -> Sub msg


port report : Encode.Value -> Cmd msg


type Msg
    = Run


allTests : List ( String, Bool )
allTests =
    RevisionTests.suite ++ UploadQueueTests.suite ++ QueueTests.suite ++ AccessTests.suite ++ ApiErrorTests.suite ++ CollectionRefreshTests.suite ++ CollectionLoadMoreTests.suite ++ WorkspaceControlTests.suite ++ SearchTests.suite ++ TagsTests.suite ++ ReactionsTests.suite ++ CollectionsTests.suite


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
